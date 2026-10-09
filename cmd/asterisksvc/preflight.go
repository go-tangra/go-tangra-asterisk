package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
	"gopkg.in/yaml.v3"

	"github.com/go-tangra/go-tangra/v4/preflight"

	"github.com/go-tangra/go-tangra-asterisk/v4/internal/config"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/pbx"
)

// dbTimeout bounds one database probe (connect + queries).
const dbTimeout = 8 * time.Second

// cdrColumns are the cdr columns the service refuses to start without
// (internal/pbx.Open).
var cdrColumns = []string{"linkedid", "uniqueid", "calldate", "channel", "dstchannel", "src", "dst", "disposition", "duration", "billsec"}

// preflightCmd checks the configuration and the environment the service
// would start in and reports every problem at once: the configuration with
// the real loader and validation, the files it references, the enrolment
// token (decoded locally, not verified), the token issuer, reachability of
// the core, the PBX databases and AMI. Nothing is changed; the token is not
// consumed.
func preflightCmd(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	p := planner{now: time.Now}
	return preflight.Main(ctx, "asterisk", args, stdout, stderr, "configs/dev.yaml", p.plan)
}

// planner builds the checks; now and issuerTLS are seams for tests.
type planner struct {
	now       func() time.Time
	issuerTLS *tls.Config // nil: system roots, like a browser reaching the portal
}

func (p planner) plan(_ context.Context, path string) []preflight.Check {
	cfg, checks, ok := loadConfig(path)
	if !ok {
		return checks
	}
	checks = append(checks, configChecks(cfg)...)
	checks = append(checks, fileChecks(cfg)...)
	enrolled := false
	if cfg.MeshEnroll.Enabled {
		var ec []preflight.Check
		ec, enrolled = p.enrollChecks(cfg)
		checks = append(checks, ec...)
	}
	checks = append(checks, reachChecks(cfg, enrolled)...)
	checks = append(checks, p.issuerChecks(cfg)...)
	return append(checks, dbChecks(cfg)...)
}

// yamlValueRE matches the values the YAML decoder quotes in type errors
// (a mistyped secret must never be printed).
var yamlValueRE = regexp.MustCompile("`[^`]*`")

// loadConfig decodes the file like the service (environment expanded,
// unknown keys refused). Unknown keys are reported one by one and the rest
// of the file is still checked; any other decode error ends the plan.
func loadConfig(path string) (config.Config, []preflight.Check, bool) {
	const name = "config: load"
	cfg, err := config.Parse(path)
	var pe config.ParseError
	var te *yaml.TypeError
	switch {
	case err == nil:
		return cfg, []preflight.Check{preflight.Static(name, preflight.Passf("%s parsed (unknown keys refused)", path))}, true
	case errors.Is(err, config.ErrUnavailable):
		return cfg, []preflight.Check{preflight.Static(name, preflight.Failf("%s is not readable", path).
			WithFix("pass the service's configuration with -config <file>"))}, false
	case errors.As(err, &pe) && errors.As(pe.Err, &te):
		var checks []preflight.Check
		for _, msg := range te.Errors {
			r := preflight.Failf("%s", yamlValueRE.ReplaceAllString(msg, "<value>"))
			if strings.Contains(msg, "not found in type") {
				r = r.WithFix("remove or rename the key: the service refuses unknown keys")
			}
			checks = append(checks, preflight.Static(name, r))
		}
		// yaml.v3 decodes everything it can around a TypeError.
		return cfg, checks, true
	default:
		msg := "not valid YAML"
		if errors.As(err, &pe) {
			msg = yamlValueRE.ReplaceAllString(pe.Err.Error(), "<value>")
		}
		return cfg, []preflight.Check{preflight.Static(name, preflight.Failf("%s: %s", path, msg).
			WithFix("fix the YAML syntax"))}, false
	}
}

// configChecks reports every validation problem and the framework's
// accepted insecure opt-outs.
func configChecks(cfg config.Config) []preflight.Check {
	const name = "config: validation"
	var checks []preflight.Check
	errs := cfg.ValidateAll()
	if len(errs) == 0 {
		checks = append(checks, preflight.Static(name, preflight.Passf("valid for env %q (production rules: %s)", cfg.Env, yesNo(cfg.IsProduction()))))
	}
	for _, err := range errs {
		r := preflight.Failf("%s", strings.TrimPrefix(err.Error(), "config: "))
		if fix := validationFix(err.Error()); fix != "" {
			r = r.WithFix("%s", fix)
		}
		checks = append(checks, preflight.Static(name, r))
	}
	for _, w := range cfg.Config.Warnings() {
		checks = append(checks, preflight.Static("config: warning", preflight.Warnf("%s", w)))
	}
	return checks
}

// validationFixes maps validation messages to remedies, first match wins.
var validationFixes = []struct{ match, fix string }{
	{"mesh_enroll requires enroll_url", "set mesh_enroll.enroll_url, lcm_grpc, tenant_id and token_file (an unset ${VAR} in the file becomes empty)"},
	{"mesh_enroll requires identity.provider", "set identity.provider: provided"},
	{"tenant_id and pbx_id", "set binding.tenant_id (the one tenant this PBX serves) and binding.pbx_id"},
	{"binding.cdr_dsn required", "set binding.cdr_dsn to the PBX's CDR database (read-only user), e.g. user:pass@tcp(pbx:3306)/asteriskcdrdb"},
	{"invalid database DSN", "use the MySQL DSN form user:pass@tcp(host:3306)/database"},
	{"module-owned and separate", "point binding.registration_dsn at a database of the module, not the PBX's CDR or config database"},
	{"invalid timezone", "use an IANA zone such as Europe/Sofia"},
	{"request_timeout must cover", "raise limits.request_timeout to at least stream_seconds"},
	{"AMI credentials/address", "set ami.address host:port, ami.username and ami.secret, or ami.enabled: false"},
	{"recording_root must be absolute", "use an absolute path for binding.recording_root"},
	{"dedicated monitoring upstream", "binding.monitoring_url must be an http(s) URL without credentials, query or fragment, with monitoring_dedicated: true"},
	{"gateway issuer/service", "set gateway.issuer to the portal's public origin (auth's issuer setting) and gateway.service"},
	{"trust_domain", "use the core's trust domain, as in the token's spiffe://<trust domain>/svc/... path"},
}

func validationFix(msg string) string {
	for _, f := range validationFixes {
		if strings.Contains(msg, f.match) {
			return f.fix
		}
	}
	return ""
}

// fileChecks checks every file and directory the configuration references.
func fileChecks(cfg config.Config) []preflight.Check {
	var checks []preflight.Check
	if cfg.Authz.Source == "file" && cfg.Authz.Path != "" {
		checks = append(checks, preflight.FileReadable("file: authz.path", cfg.Authz.Path))
	}
	if cfg.Identity.Provider == "file" {
		f := cfg.Identity.File
		checks = append(checks, preflight.FileReadable("file: identity.file.cert", f.Cert),
			preflight.FileReadable("file: identity.file.key", f.Key), preflight.FileReadable("file: identity.file.bundle", f.Bundle))
	}
	if cfg.MeshEnroll.Enabled && cfg.MeshEnroll.StateFile != "" {
		checks = append(checks, preflight.DirWritable("dir: mesh_enroll.state_file", filepath.Dir(cfg.MeshEnroll.StateFile)))
	}
	if root := cfg.Binding.RecordingRoot; root != "" {
		checks = append(checks, recordingRootCheck(root))
	}
	return checks
}

// recordingRootCheck: recordings are only read; a missing root disables the
// feature (the service retries), so it is a warning.
func recordingRootCheck(root string) preflight.Check {
	return preflight.Check{Name: "dir: binding.recording_root", Run: func(context.Context) preflight.Result {
		st, err := os.Stat(root)
		switch {
		case err != nil:
			return preflight.Warnf("%s is not accessible: recordings stay unavailable until it is", root).
				WithFix("mount the PBX recordings directory (read-only) at %s", root)
		case !st.IsDir():
			return preflight.Failf("%s is not a directory", root)
		}
		f, err := os.Open(root) // #nosec G304 -- operator-supplied directory
		if err != nil {
			return preflight.Warnf("%s is not readable by this user: recordings stay unavailable", root).
				WithFix("grant the service user read access to %s", root)
		}
		_ = f.Close()
		return preflight.Passf("%s readable", root)
	}}
}

// persistedSVID is the part of lcmidentity's state file preflight reads.
type persistedSVID struct {
	CertPEM string `json:"cert_pem"`
}

// enrollChecks reports the saved SVID and, while enrolment is still to come,
// the enrolment token. enrolled is true when a valid SVID is saved (the
// service renews over mTLS and never reads the token again).
func (p planner) enrollChecks(cfg config.Config) ([]preflight.Check, bool) {
	const name = "enrolment: state"
	m := cfg.MeshEnroll
	want := cfg.LocalSPIFFEID()
	tokenChecks := preflight.EnrollTokenChecks("enrolment token", m.TokenFile, preflight.EnrollExpect{
		TrustDomain: cfg.TrustDomain, ServiceName: cfg.ServiceName, TenantID: m.TenantID, Now: p.now})
	if m.StateFile == "" {
		return tokenChecks, false
	}
	state, err := p.savedSVID(m.StateFile, want)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return append([]preflight.Check{preflight.Static(name,
			preflight.Skipf("no saved SVID at %s yet: the first start enrols with the token", m.StateFile))}, tokenChecks...), false
	case err != nil:
		return append([]preflight.Check{preflight.Static(name,
			preflight.Warnf("saved SVID at %s is unusable (%s): the service will enrol again with the token", m.StateFile, err))}, tokenChecks...), false
	}
	checks := []preflight.Check{preflight.Static(name,
		preflight.Passf("enrolled: saved SVID %s valid until %s; renewals use mTLS, the token is not needed", want, state.NotAfter.UTC().Format("2006-01-02 15:04 UTC")))}
	for _, c := range tokenChecks {
		checks = append(checks, preflight.Static(c.Name, preflight.Skipf("enrolment already done (saved SVID valid)")))
	}
	return checks, true
}

// savedSVID returns the certificate in the lcmidentity state file when the
// service would reuse it: it names want and stays valid for over a minute.
func (p planner) savedSVID(path, want string) (*x509.Certificate, error) {
	b, err := os.ReadFile(path) // #nosec G304 -- operator-supplied state path
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, errors.New("not readable")
	}
	var rec persistedSVID
	if json.Unmarshal(b, &rec) != nil {
		return nil, errors.New("not an SVID state file")
	}
	blk, _ := pem.Decode([]byte(rec.CertPEM))
	if blk == nil {
		return nil, errors.New("no certificate")
	}
	crt, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return nil, errors.New("certificate does not parse")
	}
	if len(crt.URIs) != 1 || crt.URIs[0].String() != want {
		return nil, fmt.Errorf("certificate is not for %s", want)
	}
	if crt.NotAfter.Sub(p.now()) < time.Minute {
		return nil, fmt.Errorf("expired at %s", crt.NotAfter.UTC().Format("2006-01-02 15:04 UTC"))
	}
	return crt, nil
}

// reachChecks dials every core endpoint and PBX service the service will
// use. The enroll URL is only needed for a first enrolment, so once enrolled
// its failure is a warning.
func reachChecks(cfg config.Config, enrolled bool) []preflight.Check {
	var checks []preflight.Check
	for _, svc := range []string{cfg.Gateway.Service, "auth"} {
		if svc != "" && len(cfg.Discovery.Static[svc]) == 0 {
			checks = append(checks, preflight.Static("discovery: "+svc, preflight.Failf("no endpoint for service %q in discovery.static", svc).
				WithFix("add discovery.static.%s: [\"<core host>:<port>\"] (the core's %s gRPC port)", svc, svc)))
		}
	}
	names := make([]string, 0, len(cfg.Discovery.Static))
	for n := range cfg.Discovery.Static {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		for _, addr := range cfg.Discovery.Static[n] {
			checks = append(checks, preflight.TCPDial("reach: "+n, addr, preflight.DialTimeout))
		}
	}
	if cfg.AMI.Enabled && cfg.AMI.Address != "" {
		checks = append(checks, preflight.TCPDial("reach: ami (ami.address)", cfg.AMI.Address, preflight.DialTimeout))
	}
	if u, err := url.Parse(cfg.Binding.MonitoringURL); cfg.Binding.MonitoringURL != "" && err == nil && u.Host != "" {
		if u.Scheme == "https" {
			checks = append(checks, preflight.HTTPSProbe("reach: monitoring (binding.monitoring_url)", cfg.Binding.MonitoringURL, nil, preflight.DialTimeout))
		} else {
			addr := u.Host
			if u.Port() == "" {
				addr = net.JoinHostPort(u.Hostname(), "80")
			}
			checks = append(checks, preflight.TCPDial("reach: monitoring (binding.monitoring_url)", addr, preflight.DialTimeout))
		}
	}
	m := cfg.MeshEnroll
	if !m.Enabled {
		return checks
	}
	if m.LCMGRPCTarget != "" {
		checks = append(checks, preflight.TCPDial("reach: lcm (mesh_enroll.lcm_grpc)", m.LCMGRPCTarget, preflight.DialTimeout))
	}
	if m.EnrollURL != "" {
		// The same server verification as lcmidentity's first enrolment.
		var tc *tls.Config
		if m.Insecure {
			tc = &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13} // #nosec G402 -- mirrors mesh_enroll.insecure (development only), reported as a warning
		}
		probe := preflight.HTTPSProbe("reach: enroll (mesh_enroll.enroll_url)", m.EnrollURL, tc, preflight.DialTimeout)
		if enrolled {
			run := probe.Run
			probe.Run = func(ctx context.Context) preflight.Result {
				r := run(ctx)
				if r.Status == preflight.Fail {
					r.Status = preflight.Warn
					r.Detail += " (only needed to enrol again)"
				}
				return r
			}
		}
		checks = append(checks, probe)
	}
	return checks
}

// issuerChecks verifies gateway.issuer, which every operator token must
// carry: a wrong value lets the service start, enrol and register, then
// refuses every console request ("session ended"). The JWKS fetch proves the
// value is the portal's auth origin; the offline comparison with the enrol
// URL catches a wrong host even with -offline.
func (p planner) issuerChecks(cfg config.Config) []preflight.Check {
	var checks []preflight.Check
	if cfg.MeshEnroll.Enabled && cfg.MeshEnroll.EnrollURL != "" {
		checks = append(checks, preflight.IssuerOrigin("issuer: gateway.issuer origin", cfg.Gateway.Issuer, "enrolment URL", cfg.MeshEnroll.EnrollURL))
	}
	return append(checks, preflight.IssuerJWKS("issuer: gateway.issuer signing keys", cfg.Gateway.Issuer, p.issuerTLS, preflight.DialTimeout))
}

// dbChecks probes every configured PBX/module database. The CDR source is
// required (the service does not start without it); the others are
// optional features the service retries in the background.
func dbChecks(cfg config.Config) []preflight.Check {
	b := cfg.Binding
	var checks []preflight.Check
	if b.CDRDSN != "" {
		checks = append(checks, dbCheck("database: binding.cdr_dsn", b.CDRDSN, b.SourceTimezone, true, cdrSchema))
	}
	if b.ConfigDSN != "" {
		checks = append(checks, dbCheck("database: binding.config_dsn", b.ConfigDSN, b.SourceTimezone, false, nil))
	}
	if b.RegistrationDSN != "" {
		// registration.Open connects in UTC.
		checks = append(checks, dbCheck("database: binding.registration_dsn", b.RegistrationDSN, "UTC", false, nil))
	}
	return checks
}

// dbCheck connects with dsn exactly as the service (pbx.OpenDB) and runs
// SELECT 1, then schema. Connection, authentication and schema failures are
// reported distinctly; the password is never reported. An optional
// database's failure is a warning: the service starts without the feature.
func dbCheck(name, dsn, tz string, required bool, schema func(context.Context, *sql.DB) preflight.Result) preflight.Check {
	return preflight.Check{Name: name, Network: true, Run: func(ctx context.Context) preflight.Result {
		r := probeDB(ctx, dsn, tz, schema)
		if !required && r.Status == preflight.Fail {
			r.Status = preflight.Warn
			r.Detail += " (optional: the feature stays unavailable and is retried)"
		}
		return r
	}}
}

func probeDB(ctx context.Context, dsn, tz string, schema func(context.Context, *sql.DB) preflight.Result) preflight.Result {
	mc, err := pbx.DSNConfig(dsn, tz)
	if err != nil {
		return preflight.Failf("not a MySQL DSN").WithFix("use user:pass@tcp(host:3306)/database")
	}
	target := fmt.Sprintf("%s/%s as %s", mc.Addr, mc.DBName, mc.User)
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	// The service's driver settings (pbx.OpenDB), keeping the driver's error.
	db, err := sql.Open("mysql", mc.FormatDSN())
	if err != nil {
		return preflight.Failf("%s: the driver refused the DSN", target)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return dbFailure(target, mc.Addr, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(new(int)); err != nil {
		return dbFailure(target, mc.Addr, err)
	}
	if schema != nil {
		if r := schema(ctx, db); r.Status != preflight.Pass {
			r.Detail = target + ": " + r.Detail
			return r
		}
	}
	return preflight.Passf("%s: connected, SELECT 1 ok", target)
}

// dbFailure classifies a MySQL failure; driver errors never carry the
// password.
func dbFailure(target, addr string, err error) preflight.Result {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case 1045:
			return preflight.Failf("%s: access denied (wrong user or password, or the user may not connect from this host)", target).
				WithFix("check the DSN's user and password, and that MySQL grants this user access from this host's address")
		case 1049:
			return preflight.Failf("%s: unknown database", target).WithFix("check the database name in the DSN")
		case 1044, 1142:
			return preflight.Failf("%s: the user lacks privileges (%d)", target, me.Number).
				WithFix("grant the user SELECT on the database")
		}
		return preflight.Failf("%s: MySQL error %d", target, me.Number)
	}
	if errors.Is(err, mysql.ErrInvalidConn) || errors.Is(err, context.DeadlineExceeded) {
		return preflight.Failf("%s: no answer within %s", target, dbTimeout).
			WithFix("check that %s is reachable from this host and MySQL listens there", addr)
	}
	var ne net.Error
	var oe *net.OpError
	if errors.As(err, &oe) || errors.As(err, &ne) {
		return preflight.DialFailure(addr, err, preflight.DialTimeout)
	}
	return preflight.Failf("%s: %s", target, oneLine(err))
}

// cdrSchema mirrors pbx.Open: a cdr table with every required column.
func cdrSchema(ctx context.Context, db *sql.DB) preflight.Result {
	rows, err := db.QueryContext(ctx, "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='cdr'")
	if err != nil {
		return preflight.Failf("cannot read the cdr table's columns")
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil {
			have[c] = true
		}
	}
	if len(have) == 0 {
		return preflight.Failf("no cdr table in this database").
			WithFix("point binding.cdr_dsn at the PBX's CDR database (usually asteriskcdrdb)")
	}
	var missing []string
	for _, c := range cdrColumns {
		if !have[c] {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return preflight.Failf("cdr table lacks required columns: %s", strings.Join(missing, ", ")).
			WithFix("the service needs Asterisk's standard cdr columns (linkedid requires Asterisk 1.8+ CDR)")
	}
	return preflight.Passf("cdr table has every required column")
}

func oneLine(err error) string { return strings.Join(strings.Fields(err.Error()), " ") }

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
