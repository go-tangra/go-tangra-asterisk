//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"embed"
	"github.com/go-sql-driver/mysql"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/cdr"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/config"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/pbx"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/registration"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/stats"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

//go:embed fixtures/schema.sql
var fixtures embed.FS

func fixture(t *testing.T) (config.Config, *pbx.Pools, *sql.DB) {
	t.Helper()
	adminDSN := os.Getenv("ASTERISK_FIXTURE_ADMIN_DSN")
	readerDSN := os.Getenv("ASTERISK_FIXTURE_CDR_DSN")
	if adminDSN == "" || readerDSN == "" {
		t.Fatal("set ASTERISK_FIXTURE_ADMIN_DSN and ASTERISK_FIXTURE_CDR_DSN for dedicated MySQL fixture")
	}
	for _, dsn := range []string{adminDSN, readerDSN} {
		c, e := mysql.ParseDSN(dsn)
		if e != nil || c.DBName != "asterisk_fixture_cdr" {
			t.Fatal("fixture writes restricted to asterisk_fixture_cdr")
		}
	}
	ctx := context.Background()
	admin, e := pbx.OpenDB(ctx, adminDSN, "UTC")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { admin.Close() })
	raw, _ := fixtures.ReadFile("fixtures/schema.sql")
	for _, q := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(q) != "" {
			if _, e = admin.ExecContext(ctx, q); e != nil {
				t.Fatal(e)
			}
		}
	}
	for _, table := range []string{"cdr", "cel"} {
		if _, e = admin.ExecContext(ctx, "DELETE FROM "+table); e != nil {
			t.Fatal(e)
		}
	}
	c := config.Default()
	c.Binding.TenantID = "t"
	c.Binding.PBXID = "p"
	c.Binding.CDRDSN = readerDSN
	c.Binding.SourceTimezone = "UTC"
	c.Binding.Timezone = "Europe/Sofia"
	c.Binding.RegistrationDSN = os.Getenv("ASTERISK_FIXTURE_REGISTRATION_DSN")
	if c.Binding.RegistrationDSN != "" {
		owned, e := mysql.ParseDSN(c.Binding.RegistrationDSN)
		if e != nil || owned.DBName != "asterisk_fixture_registration" {
			t.Fatal("owned fixture must use asterisk_fixture_registration")
		}
	}
	p, e := pbx.Open(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	return c, p, admin
}
func seed(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	tx, e := db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	stmt, e := tx.Prepare("INSERT INTO cdr(linkedid,uniqueid,sequence,calldate,channel,dstchannel,src,dst,disposition,duration,billsec) VALUES(?,?,?,?,'PJSIP/trunk-1','PJSIP/01-1','1234','600',?,10,7)")
	if e != nil {
		t.Fatal(e)
	}
	defer stmt.Close()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		disposition := "ANSWERED"
		if i%2 == 1 {
			disposition = "BUSY"
		}
		if _, e = stmt.Exec(fmtID(i/2), fmtID(i), i, start.Add(time.Duration(i/2)*time.Second), disposition); e != nil {
			t.Fatal(e)
		}
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
}
func fmtID(i int) string { return "fixture-" + strconv.Itoa(i) }
func TestMySQLLogicalParityAndReadOnly(t *testing.T) {
	c, p, admin := fixture(t)
	seed(t, admin, 4)
	ctx := context.Background()
	r := &cdr.Repository{Pools: p}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	list, e := r.List(ctx, "t", cdr.Filter{From: from, To: to, Page: 1, PageSize: 25, Sort: "start", Order: "asc"})
	if e != nil {
		t.Fatal(e)
	}
	if list.Total != 2 || list.Items[0].Disposition != "ANSWERED" || list.Items[0].LegCount != 2 {
		t.Fatalf("bad logical list %+v", list)
	}
	if _, e = r.Detail(ctx, "foreign", list.Items[0].LinkedID); e == nil {
		t.Fatal("foreign PBX read")
	}
	if _, e = p.CDR.ExecContext(ctx, "INSERT INTO cdr(linkedid) VALUES('forbidden')"); e == nil {
		t.Fatal("source credential can write")
	}
	loc, _ := time.LoadLocation("Europe/Sofia")
	reports := stats.Repository{CDR: r, Location: loc}
	v, e := reports.Overview(ctx, "t", from, to, "day")
	if e != nil || v.Total != 2 || v.Answered != 2 {
		t.Fatalf("bad reports %+v %v", v, e)
	}
	if c.Binding.RegistrationDSN == "" {
		t.Fatal("registration fixture DSN required for full acceptance")
	}
	if e = registration.Bootstrap(ctx, c); e != nil {
		t.Fatal(e)
	}
	store, e := registration.Open(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	defer store.DB.Close()
	if _, e = store.DB.ExecContext(ctx, "DELETE FROM pjsip_registration_events"); e != nil {
		t.Fatal(e)
	}
	if _, e = store.DB.ExecContext(ctx, "DELETE FROM asterisk_observation_gaps"); e != nil {
		t.Fatal(e)
	}
	event := registration.Event{Endpoint: "01", Contact: "sip:a", Time: from, Status: "Created", Expire: to}
	if e = store.Append(ctx, event); e != nil {
		t.Fatal(e)
	}
	statuses, _, e := store.At(ctx, "t", "01", from.Add(time.Minute))
	if e != nil || len(statuses) != 1 || !statuses[0].Registered {
		t.Fatalf("stored registration %v %v", statuses, e)
	}
	event.Contact = "sip:b"
	event.ID = 0
	event.Expire = to.Add(time.Hour)
	if e = store.Append(ctx, event); e != nil {
		t.Fatal(e)
	}
	event.Contact = "sip:a"
	event.Status = "Removed"
	event.Time = from.Add(time.Minute)
	if e = store.Append(ctx, event); e != nil {
		t.Fatal(e)
	}
	statuses, _, e = store.At(ctx, "t", "01", from.Add(2*time.Minute))
	if e != nil || !statuses[0].Registered {
		t.Fatal("second contact did not survive removal")
	}
	now := time.Now().UTC()
	if e = store.BeginGap(ctx); e != nil {
		t.Fatal(e)
	}
	statuses, gaps, e := store.At(ctx, "t", "01", now.Add(time.Second))
	if e != nil || len(gaps) == 0 || statuses[0].Certainty != "uncertain" {
		t.Fatal("stored gap missing")
	}
	if e = store.RecoverGap(ctx); e != nil {
		t.Fatal(e)
	}
	foreign := c
	foreign.Binding.TenantID = "other"
	if registration.Bootstrap(ctx, foreign) == nil {
		t.Fatal("foreign tenant adopted owned registration database")
	}
	if _, _, e = store.At(ctx, "foreign", "01", now); e == nil {
		t.Fatal("registration tenant guard bypassed")
	}

}
