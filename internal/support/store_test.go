package support

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sqle "github.com/dolthub/go-mysql-server"
	"github.com/dolthub/go-mysql-server/memory"
	"github.com/dolthub/go-mysql-server/server"
	gms "github.com/dolthub/go-mysql-server/sql"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/pbx"
)

// mysqlServer starts an in-memory MySQL-compatible server with the given
// databases and returns a DSN prefix ("root:@tcp(host:port)/").
func mysqlServer(t *testing.T, names ...string) string {
	t.Helper()
	var dbs []*memory.Database
	for _, n := range names {
		db := memory.NewDatabase(n)
		db.BaseDatabase.EnablePrimaryKeyIndexes()
		dbs = append(dbs, db)
	}
	pro := memory.NewDBProvider(dbsAsAny(dbs)...)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	s, err := server.NewServer(server.Config{Protocol: "tcp", Address: addr}, sqle.NewDefault(pro), gms.NewContext, memory.NewSessionBuilder(pro), nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = s.Start() }()
	t.Cleanup(func() { s.Close() })
	return "root:@tcp(" + addr + ")/"
}

func open(t *testing.T, dsn, tz string) *sql.DB {
	t.Helper()
	var db *sql.DB
	var err error
	for i := 0; i < 50; i++ { // the server starts asynchronously
		if db, err = pbx.OpenDB(context.Background(), dsn, tz); err == nil {
			t.Cleanup(func() { db.Close() })
			return db
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(err)
	return nil
}

func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// pbxWithGolden fills a PBX source database with every golden fixture (CEL
// ids in fixture order) and a FreePBX configuration like the survey's.
func pbxWithGolden(t *testing.T, cdrDB, cfgDB *sql.DB) {
	exec(t, cdrDB, "CREATE TABLE cdr (calldate DATETIME NOT NULL, clid VARCHAR(80) NOT NULL DEFAULT '', src VARCHAR(80) NOT NULL DEFAULT '', dst VARCHAR(80) NOT NULL DEFAULT '', dcontext VARCHAR(80) NOT NULL DEFAULT '', channel VARCHAR(80) NOT NULL DEFAULT '', dstchannel VARCHAR(80) NOT NULL DEFAULT '', lastapp VARCHAR(80) NOT NULL DEFAULT '', lastdata VARCHAR(80) NOT NULL DEFAULT '', duration INT NOT NULL DEFAULT 0, billsec INT NOT NULL DEFAULT 0, disposition VARCHAR(45) NOT NULL DEFAULT '', uniqueid VARCHAR(32) NOT NULL DEFAULT '', recordingfile VARCHAR(255) NOT NULL DEFAULT '', linkedid VARCHAR(32) NOT NULL DEFAULT '', sequence INT NOT NULL DEFAULT 0, rtpqos VARCHAR(512) NULL, peerrtpqos VARCHAR(512) NULL, KEY (linkedid))")
	exec(t, cdrDB, "CREATE TABLE cel (id INT NOT NULL AUTO_INCREMENT PRIMARY KEY, eventtype VARCHAR(30) NOT NULL, eventtime DATETIME NOT NULL, cid_num VARCHAR(80) NOT NULL DEFAULT '', exten VARCHAR(80) NOT NULL DEFAULT '', context VARCHAR(80) NOT NULL DEFAULT '', channame VARCHAR(80) NOT NULL DEFAULT '', appname VARCHAR(80) NOT NULL DEFAULT '', appdata VARCHAR(1024) NULL, uniqueid VARCHAR(32) NOT NULL DEFAULT '', linkedid VARCHAR(32) NOT NULL DEFAULT '', peer VARCHAR(255) NOT NULL DEFAULT '', extra VARCHAR(512) NOT NULL DEFAULT '', KEY (linkedid))")
	dirs, _ := filepath.Glob("testdata/golden/*")
	for _, dir := range dirs {
		cdrs, cels := loadGolden(t, dir)
		for _, r := range cdrs {
			rec := ""
			if r.Recorded {
				rec = "2026/01/08/x.wav"
			}
			exec(t, cdrDB, "INSERT INTO cdr (calldate,src,dst,dcontext,channel,dstchannel,lastapp,duration,billsec,disposition,uniqueid,recordingfile,linkedid,sequence,rtpqos,peerrtpqos) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
				r.Start, r.Src, r.Dst, r.DContext, r.Channel, r.DstChannel, r.LastApp, r.Duration, r.Billsec, r.Disposition, r.UniqueID, rec, r.LinkedID, r.Sequence, r.RTPQoS, r.PeerRTPQoS)
		}
		for _, e := range cels {
			exec(t, cdrDB, "INSERT INTO cel (eventtype,eventtime,context,channame,appname,appdata,uniqueid,linkedid,extra) VALUES (?,?,?,?,?,?,?,?,?)", e.Type, e.Time, e.Context, e.Channel, e.App, e.AppData, e.UniqueID, e.LinkedID, e.Extra)
		}
	}
	exec(t, cfgDB, "CREATE TABLE trunks (trunkid INT PRIMARY KEY, channelid VARCHAR(80))")
	exec(t, cfgDB, "INSERT INTO trunks VALUES (1,'Kamailio'),(2,'ITD'),(3,'elevenlabs')")
	exec(t, cfgDB, "CREATE TABLE ringgroups (grpnum VARCHAR(20) PRIMARY KEY, strategy VARCHAR(50), grptime SMALLINT, grplist VARCHAR(255))")
	exec(t, cfgDB, "INSERT INTO ringgroups VALUES ('600','ringall',300,'22-21-16-18-30-35-36-40-41-45-48-49-51-54-55-201-202-0888123456#')")
	exec(t, cfgDB, "CREATE TABLE announcement (announcement_id INT PRIMARY KEY, description VARCHAR(50), post_dest VARCHAR(255))")
	exec(t, cfgDB, "INSERT INTO announcement VALUES (1,'Welcome','ext-group,600,1'),(2,'Other','ext-local,vms111,1')")
}

func TestIngestAndReport(t *testing.T) {
	base := mysqlServer(t, "asteriskcdrdb", "asterisk", "owned")
	cdrDB := open(t, base+"asteriskcdrdb", "Europe/Sofia")
	cfgDB := open(t, base+"asterisk", "Europe/Sofia")
	ownedDB := open(t, base+"owned", "UTC")
	pbxWithGolden(t, cdrDB, cfgDB)
	ctx := context.Background()
	src := &Source{CDR: cdrDB, Config: cfgDB}
	if err := src.Probe(ctx); err != nil {
		t.Fatal(err)
	}
	store := &Store{DB: ownedDB, Tenant: "t"}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil { // idempotent
		t.Fatal(err)
	}
	svc := &Service{Source: src, Store: store, Exclude: []string{"201-211"}, Callback: 2 * time.Hour}
	if n, err := svc.Step(ctx); err != nil || n != 8 {
		t.Fatalf("first step read %d calls: %v", n, err)
	}
	if n, err := svc.Step(ctx); err != nil || n != 0 {
		t.Fatalf("caught up, yet read %d calls: %v", n, err)
	}
	if team := strings.Join(svc.TeamMembers(ctx), ","); team != "16,18,21,22,30,35,36,40,41,45,48,49,51,54,55" {
		t.Fatalf("team %s", team)
	}
	sofia, _ := time.LoadLocation("Europe/Sofia")
	from := time.Date(2026, 1, 8, 0, 0, 0, 0, sofia)
	to := from.AddDate(0, 0, 1)
	calls, err := svc.Report(ctx, "t", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Report(ctx, "other", from, to); err != ErrDenied {
		t.Fatalf("other tenant: %v", err)
	}
	if len(calls) != 7 { // no-dial is not stored
		t.Fatalf("stored %d calls", len(calls))
	}
	sum := Summarize(calls, from, to, svc.Callback)
	got := fmt.Sprintf("in=%d ans=%d early=%d lost=%v out=%d/%d wait=%v", sum.Inbound.Calls, sum.Inbound.Answered, sum.Inbound.AbandonedEarly, sum.Inbound.LostBy, sum.Outbound.Attempts, sum.Outbound.Connected, *sum.Inbound.Wait.Median)
	if want := "in=4 ans=1 early=1 lost=map[caller-hung-up-while-ringing:1 no-agent-available:1] out=2/1 wait=23"; got != want {
		t.Fatalf("summary\n got %s\nwant %s", got, want)
	}
	agents := map[string]*AgentStat{}
	for _, a := range Agents(calls, from, to, svc.Callback, svc.TeamMembers(ctx), nil) {
		agents[a.Extension] = a
	}
	a49, a41, a16 := agents["49"], agents["41"], agents["16"]
	if a49.Inbound.Answered != 1 || a49.Inbound.Talk != 72 || *a49.Inbound.TimeToAnswer.Median != 23 || a49.Inbound.AnswerShare != 1 {
		t.Fatalf("49: %+v", a49.Inbound)
	}
	if a41.Inbound.Declined != 2 || a41.Inbound.Offered != 2 {
		t.Fatalf("41 declined both calls it rang for: %+v", a41.Inbound)
	}
	if a16 == nil || !a16.Member || a16.Inbound.Offered != 0 {
		t.Fatalf("idle members are listed: %+v", a16)
	}
	if o := agents["45"].Outbound; o.Attempts != 1 || o.Connected != 1 || o.Talk != 26 {
		t.Fatalf("45 outbound: %+v", o)
	}
	lost := LostCalls(calls, from, to, svc.Callback)
	if len(lost) != 3 || lost[0].Reason != LostNoAgent || len(lost[1].Rang) != 0 && lost[1].Outcome != OutcomeAbandonedEarly {
		t.Fatalf("lost calls: %+v", lost)
	}
}

func dbsAsAny(dbs []*memory.Database) []gms.Database {
	out := make([]gms.Database, len(dbs))
	for i, db := range dbs {
		out[i] = db
	}
	return out
}
