package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/go-tangra/go-tangra/v4/preflight"
)

var clock = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

const (
	meshTenant = "00000000-0000-0000-0000-000000000001"
	dbPassword = "pw-s3cret"
)

// env is a temporary remote install: policy, token and the endpoints the
// config points at.
type env struct {
	Dir, TokenFile, StateFile                string
	Gateway, Auth, LCM, EnrollURL, Issuer    string
	CDRDSN, RegistrationDSN, Recordings, Ext string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "policy.yaml"), []byte("rules: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	e := &env{Dir: dir, StateFile: filepath.Join(dir, "state", "svid.json"),
		Gateway: closedAddr(t), Auth: closedAddr(t), LCM: closedAddr(t),
		EnrollURL: "https://" + closedAddr(t) + "/api/lcm/v1/enroll", Issuer: "https://" + closedAddr(t),
		CDRDSN:          "cdr:" + dbPassword + "@tcp(" + closedAddr(t) + ")/asteriskcdrdb",
		RegistrationDSN: "reg:" + dbPassword + "@tcp(" + closedAddr(t) + ")/asterisk_registration",
		Recordings:      dir}
	e.TokenFile = filepath.Join(dir, "token")
	if err := os.WriteFile(e.TokenFile, []byte(token(t, clock.Add(20*time.Minute), "spiffe://infra.example.org/svc/asterisk")), 0o600); err != nil {
		t.Fatal(err)
	}
	return e
}

var configTemplate = template.Must(template.New("c").Parse(`service_name: asterisk
trust_domain: infra.example.org
env: development
identity: { provider: provided }
mesh_enroll:
  enabled: true
  enroll_url: {{.EnrollURL}}
  lcm_grpc: {{.LCM}}
  tenant_id: ` + meshTenant + `
  token_file: {{.TokenFile}}
  state_file: {{.StateFile}}
authz: { source: file, path: {{.Dir}}/policy.yaml }
server: { http_addr: 127.0.0.1:0, grpc_addr: 127.0.0.1:0 }
admin: { addr: 127.0.0.1:0 }
discovery:
  static:
    auth: ["{{.Auth}}"]
    gateway: ["{{.Gateway}}"]
binding:
  tenant_id: 0b2f6a1e-4c55-4c8e-9d1a-000000000a0a
  pbx_id: pbx-1
  cdr_dsn: "{{.CDRDSN}}"
  registration_dsn: "{{.RegistrationDSN}}"
  recording_root: {{.Recordings}}
ami: { enabled: false }
gateway: { service: gateway, issuer: "{{.Issuer}}" }
{{.Ext}}`))

func (e *env) config(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	if err := configTemplate.Execute(&b, e); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(e.Dir, "config.yaml")
	if err := os.WriteFile(p, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// token is an enrolment token as auth mints it (signature not verified by preflight).
func token(t *testing.T, exp time.Time, paths ...string) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(map[string]string{"alg": "EdDSA"}) + "." + enc(map[string]any{"aud": "lcm", "tid": meshTenant, "spiffe_paths": paths,
		"iat": exp.Add(-30 * time.Minute).Unix(), "nbf": exp.Add(-30 * time.Minute).Unix(), "exp": exp.Unix()}) + ".c2ln"
}

func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func listen(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l.Addr().String()
}

// portal serves the enrol endpoint (405 to GET) and the platform JWKS, like
// the core's gateway; trust is a TLS config that trusts it.
func portal(t *testing.T) (*httptest.Server, *tls.Config) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == preflight.JWKSPath {
			_, _ = io.WriteString(w, `{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"k1","x":"J-dVacDYm0zmy2-X1K6XQWwsCnsjz5FQQ7K8U3wqHPE","use":"sig"}]}`)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	t.Cleanup(srv.Close)
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	return srv, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
}

func results(p planner, path string) []preflight.Result {
	return preflight.Run(context.Background(), p.plan(context.Background(), path))
}

func expect(t *testing.T, res []preflight.Result, name string, status preflight.Status, detail string) preflight.Result {
	t.Helper()
	for _, r := range res {
		if r.Name == name && r.Status == status && strings.Contains(r.Detail, detail) {
			return r
		}
	}
	t.Fatalf("no %s %q containing %q in:\n%s", status, name, detail, dump(res))
	return preflight.Result{}
}

func dump(res []preflight.Result) string {
	var b strings.Builder
	for _, r := range res {
		fmt.Fprintf(&b, "%s %s: %s | %s\n", r.Status, r.Name, r.Detail, r.Fix)
	}
	return b.String()
}

var testPlanner = planner{now: func() time.Time { return clock }}

// The core and the portal answer; only the PBX databases are absent.
func TestPreflightHealthyCore(t *testing.T) {
	e := newEnv(t)
	e.Gateway, e.Auth, e.LCM = listen(t), listen(t), listen(t)
	srv, trust := portal(t)
	e.EnrollURL, e.Issuer = srv.URL+"/api/lcm/v1/enroll", srv.URL
	p := testPlanner
	p.issuerTLS = trust
	res := results(p, e.config(t))
	for _, r := range res {
		// The test portal's certificate is not in the system roots the enrol probe uses.
		if r.Status == preflight.Fail && r.Name != "database: binding.cdr_dsn" && r.Name != "reach: enroll (mesh_enroll.enroll_url)" {
			t.Errorf("unexpected failure %s: %s", r.Name, r.Detail)
		}
	}
	expect(t, res, "config: load", preflight.Pass, "parsed")
	expect(t, res, "config: validation", preflight.Pass, `valid for env "development"`)
	expect(t, res, "file: authz.path", preflight.Pass, "readable")
	expect(t, res, "dir: mesh_enroll.state_file", preflight.Pass, "writable")
	expect(t, res, "dir: binding.recording_root", preflight.Pass, "readable")
	expect(t, res, "enrolment: state", preflight.Skip, "no saved SVID")
	expect(t, res, "enrolment token: expiry", preflight.Pass, "expires in 20 min")
	expect(t, res, "enrolment token: identity", preflight.Pass, "spiffe://infra.example.org/svc/asterisk")
	expect(t, res, "reach: gateway", preflight.Pass, "reachable")
	expect(t, res, "reach: auth", preflight.Pass, "reachable")
	expect(t, res, "reach: lcm (mesh_enroll.lcm_grpc)", preflight.Pass, "reachable")
	expect(t, res, "reach: enroll (mesh_enroll.enroll_url)", preflight.Fail, "TLS: certificate signed by an authority this host does not trust")
	expect(t, res, "issuer: gateway.issuer origin", preflight.Pass, "is the enrolment URL's origin")
	expect(t, res, "issuer: gateway.issuer signing keys", preflight.Pass, "publishes 1 platform signing key")
	expect(t, res, "database: binding.cdr_dsn", preflight.Fail, "connection refused")
	expect(t, res, "database: binding.registration_dsn", preflight.Warn, "optional: the feature stays unavailable")
	if strings.Contains(dump(res), dbPassword) {
		t.Fatal("database passwords must never be reported")
	}
}

// Everything the service would refuse is reported in one run: unset
// environment, a wrong issuer, the unreachable core.
func TestPreflightReportsEveryProblem(t *testing.T) {
	e := newEnv(t)
	e.Issuer = "${ASTERISK_AUTH_ISSUER}" // unset: expands to ""
	e.Ext = "query_timeout_seconds: 99\nstray_key: 1\n"
	res := results(testPlanner, e.config(t))
	r := expect(t, res, "config: load", preflight.Fail, "field stray_key not found")
	if !strings.Contains(r.Fix, "remove or rename") {
		t.Fatalf("fix %q", r.Fix)
	}
	expect(t, res, "config: validation", preflight.Fail, "invalid query/stream bounds")
	r = expect(t, res, "config: validation", preflight.Fail, "gateway issuer/service required")
	if !strings.Contains(r.Fix, "portal's public origin") {
		t.Fatalf("fix %q", r.Fix)
	}
	expect(t, res, "issuer: gateway.issuer signing keys", preflight.Fail, "not an https origin")
	expect(t, res, "reach: gateway", preflight.Fail, "connection refused")
	expect(t, res, "reach: lcm (mesh_enroll.lcm_grpc)", preflight.Fail, "connection refused")
	var out, errOut bytes.Buffer
	if code := preflight.Main(context.Background(), "asterisk", []string{"-config", e.config(t)}, &out, &errOut, "", testPlanner.plan); code != 1 {
		t.Fatalf("exit %d:\n%s%s", code, out.String(), errOut.String())
	}
}

// The remote sms-gw incident, for asterisk: the issuer is not the portal
// origin auth signs with. -offline still flags the wrong host.
func TestPreflightWrongIssuer(t *testing.T) {
	srv, trust := portal(t)
	p := testPlanner
	p.issuerTLS = trust
	e := newEnv(t)
	e.EnrollURL, e.Issuer = srv.URL+"/api/lcm/v1/enroll", "https://"+closedAddr(t)
	res := results(p, e.config(t))
	expect(t, res, "issuer: gateway.issuer origin", preflight.Warn, "differs from the enrolment URL's origin")
	r := expect(t, res, "issuer: gateway.issuer signing keys", preflight.Fail, "connection refused")
	if !strings.Contains(r.Fix, "portal's public origin") {
		t.Fatalf("fix %q", r.Fix)
	}
	e.EnrollURL, e.Issuer = "https://portal.example.org:8443/api/lcm/v1/enroll", "https://localhost:8443"
	var out bytes.Buffer
	preflight.Main(context.Background(), "asterisk", []string{"-config", e.config(t), "-offline"}, &out, io.Discard, "", testPlanner.plan)
	if !strings.Contains(out.String(), "issuer origin https://localhost:8443 differs from the enrolment URL's origin https://portal.example.org:8443") {
		t.Fatalf("offline run:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "database: binding.cdr_dsn               offline: not contacted") {
		t.Fatalf("-offline must skip the database probes:\n%s", out.String())
	}
}

// A mistyped value is never echoed (it may be a secret).
func TestPreflightNeverEchoesValues(t *testing.T) {
	e := newEnv(t)
	e.Ext = "stream_seconds: s3cret-typed-here\n"
	res := results(testPlanner, e.config(t))
	expect(t, res, "config: load", preflight.Fail, "<value>")
	if strings.Contains(dump(res), "s3cret-typed-here") {
		t.Fatalf("value echoed:\n%s", dump(res))
	}
}
