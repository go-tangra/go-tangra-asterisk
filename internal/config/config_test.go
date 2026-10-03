package config

import "testing"

func TestRejectStoreAliasingDifferentCredentials(t *testing.T) {
	c := Default()
	c.Gateway.Issuer = "https://auth.example.org"
	c.TrustDomain = "test.local"
	c.Authz.Path = "policy"
	c.Binding.TenantID = "t"
	c.Binding.PBXID = "p"
	c.Binding.CDRDSN = "reader:x@tcp(mysql:3306)/cdr"
	c.Binding.RegistrationDSN = "writer:y@tcp(mysql:3306)/cdr"
	if c.Validate() == nil {
		t.Fatal("writable store aliases source")
	}
	c.Binding.RegistrationDSN = "writer:y@tcp(mysql:3306)/owned"
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
}

// mesh_enroll needs its endpoints and token, and identity.provider provided;
// retention is bounded.
func TestMeshEnrollAndRetention(t *testing.T) {
	base := func() Config {
		c := Default()
		c.Gateway.Issuer = "https://auth.example.org"
		c.TrustDomain = "test.local"
		c.Authz.Path = "policy"
		c.Binding.TenantID = "t"
		c.Binding.PBXID = "p"
		c.Binding.CDRDSN = "reader:x@tcp(mysql:3306)/cdr"
		return c
	}
	c := base()
	if e := c.Validate(); e != nil {
		t.Fatalf("base: %v", e)
	}
	c.MeshEnroll = MeshEnroll{Enabled: true, EnrollURL: "https://gateway:8443/api/lcm/v1/enroll", LCMGRPCTarget: "lcm:9945", TenantID: "t", TokenFile: "/tokens/asterisk.token"}
	if c.Validate() == nil {
		t.Fatal("mesh_enroll accepted without identity.provider provided")
	}
	c.Identity.Provider = "provided"
	if e := c.Validate(); e != nil {
		t.Fatalf("mesh_enroll: %v", e)
	}
	c.MeshEnroll.TokenFile = ""
	if c.Validate() == nil {
		t.Fatal("mesh_enroll accepted without token_file")
	}
	for _, d := range []int{-1, 3651} {
		c := base()
		c.Binding.RegistrationRetentionDays = d
		if c.Validate() == nil {
			t.Fatalf("retention %d accepted", d)
		}
	}
	if Default().Binding.RegistrationRetentionDays != 400 {
		t.Fatal("default retention")
	}
}

// The shipped dev configuration loads and validates with lcm enrollment.
func TestDevConfigLoads(t *testing.T) {
	for k, v := range map[string]string{
		"ASTERISK_TENANT_ID": "t", "ASTERISK_PBX_ID": "p", "ASTERISK_CDR_DSN": "reader:x@tcp(mysql:3306)/asteriskcdrdb",
		"ASTERISK_AUTH_TARGET": "auth:9543", "ASTERISK_AUTH_ISSUER": "https://auth.example.org", "ASTERISK_PORTAL_TARGET": "gateway:9643",
		"ASTERISK_ENROLL_URL": "https://gateway:8443/api/lcm/v1/enroll", "ASTERISK_LCM_GRPC": "lcm:9945",
		"ASTERISK_ENROLL_TENANT_ID": "t", "ASTERISK_ENROLL_TOKEN_FILE": "/run/secrets/asterisk.token", "ASTERISK_ENROLL_STATE_FILE": "/var/lib/asterisk-module/svid.json",
	} {
		t.Setenv(k, v)
	}
	c, e := Load("../../configs/dev.yaml")
	if e != nil {
		t.Fatalf("load: %v", e)
	}
	if !c.MeshEnroll.Enabled || c.Identity.Provider != "provided" || c.MeshEnroll.TokenFile != "/run/secrets/asterisk.token" {
		t.Fatalf("mesh_enroll = %+v, provider %q", c.MeshEnroll, c.Identity.Provider)
	}
}
