package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-tangra/go-tangra-asterisk/v4/internal/config"
	"github.com/go-tangra/go-tangra-asterisk/v4/pkg/asteriskmanifest"
)

func TestCataloguePermissions(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := cataloguePermissions(&out, &errOut); code != 0 {
		t.Fatal(errOut.String())
	}
	var perms []string
	if err := json.Unmarshal(out.Bytes(), &perms); err != nil {
		t.Fatal(err)
	}
	m, _ := asteriskmanifest.Manifest()
	if len(perms) == 0 || len(perms) != len(m.Permissions) || !strings.Contains(strings.Join(perms, ","), "calls:read") {
		t.Fatalf("%v", perms)
	}
}

var placeholderRE = regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*)\}`)

// The join bundle's templates, rendered with values of the shape the gateway
// fills in (core values and host inputs), are a valid production
// configuration and policy.
func TestJoinBundleRendersToValidProductionConfig(t *testing.T) {
	values := map[string]string{
		"TRUST_DOMAIN": "infra.example.org", "GATEWAY_ISSUER": "https://portal.example.org:8443",
		"LCM_ENROLL_URL": "https://portal.example.org:8443/api/lcm/v1/enroll", "LCM_GRPC": "portal.example.org:9945",
		"GATEWAY_GRPC": "portal.example.org:9643", "AUTH_GRPC": "portal.example.org:9543", "MESH_TENANT_ID": "00000000-0000-0000-0000-000000000001",
		"ASTERISK_TENANT_ID": "0b2f6a1e-4c55-4c8e-9d1a-000000000a0a", "ASTERISK_PBX_ID": "pbx-1",
		"ASTERISK_CDR_DSN": "cdr:S3cret-pw@tcp(pbx.example.org:3306)/asteriskcdrdb",
	}
	dir := t.TempDir()
	for _, name := range []string{"config.yaml", "policy.yaml"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "bundle", name))
		if err != nil {
			t.Fatal(err)
		}
		var missing []string
		rendered := placeholderRE.ReplaceAllStringFunc(string(raw), func(m string) string {
			k := placeholderRE.FindStringSubmatch(m)[1]
			v, ok := values[k]
			if !ok {
				missing = append(missing, k)
			}
			return v
		})
		if len(missing) > 0 {
			t.Fatalf("%s uses placeholders the gateway does not fill: %v", name, missing)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(rendered), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("rendered config invalid: %v", err)
	}
	if !cfg.IsProduction() || cfg.Gateway.Issuer != "https://portal.example.org:8443" || cfg.Binding.TenantID != values["ASTERISK_TENANT_ID"] {
		t.Fatalf("%+v %+v", cfg.Gateway, cfg.Binding.TenantID)
	}
	pol, _ := os.ReadFile(filepath.Join(dir, "policy.yaml"))
	if !strings.Contains(string(pol), "spiffe://infra.example.org/svc/gateway") {
		t.Fatal(string(pol))
	}
}
