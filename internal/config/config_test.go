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

// Without an explicit enabled, AMI follows its address: the join bundle
// renders an empty address for a host without AMI. An address needs the
// credentials; an explicit enabled: false wins.
func TestAMIFollowsAddress(t *testing.T) {
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
	if c.AMI.On() || c.Validate() != nil {
		t.Fatal("no address: AMI must be off and valid")
	}
	c.AMI = AMI{Address: "127.0.0.1:5038", Username: "tangra", Secret: "s"}
	if !c.AMI.On() || c.Validate() != nil {
		t.Fatal("address with credentials: AMI on and valid")
	}
	c.AMI.Secret = ""
	if c.Validate() == nil {
		t.Fatal("address without a secret accepted")
	}
	off := false
	c.AMI = AMI{Address: "127.0.0.1:5038", Enabled: &off}
	if c.AMI.On() || c.Validate() != nil {
		t.Fatal("enabled: false must win over the address")
	}
	on := true
	c.AMI = AMI{Enabled: &on}
	if c.Validate() == nil {
		t.Fatal("enabled: true without an address accepted")
	}
}

// The join bundle's config.yaml, rendered as the gateway does (every input
// set, optional ones possibly empty), loads and validates: empty optional
// sources leave their features off, given ones turn them on.
func TestJoinBundleConfigRenders(t *testing.T) {
	render := func(t *testing.T, optional map[string]string) Config {
		t.Helper()
		for k, v := range map[string]string{
			"TRUST_DOMAIN": "infra.example", "LCM_ENROLL_URL": "https://portal.example:8443/api/lcm/v1/enroll", "LCM_GRPC": "portal.example:9945",
			"MESH_TENANT_ID": "00000000-0000-0000-0000-000000000001", "AUTH_GRPC": "portal.example:9543", "GATEWAY_GRPC": "portal.example:9643",
			"GATEWAY_ISSUER": "https://portal.example:8443", "ASTERISK_TENANT_ID": "00000000-0000-0000-0000-000000000001", "ASTERISK_PBX_ID": "pbx-1",
			"ASTERISK_CDR_DSN": "ro:pw@tcp(localhost:3306)/asteriskcdrdb",
			"ASTERISK_CONFIG_DSN": "", "ASTERISK_REGISTRATION_DSN": "", "ASTERISK_AMI_ADDRESS": "", "ASTERISK_AMI_USERNAME": "", "ASTERISK_AMI_SECRET": "",
		} {
			t.Setenv(k, v)
		}
		for k, v := range optional {
			t.Setenv(k, v)
		}
		c, e := Load("../../deploy/bundle/config.yaml")
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	t.Run("optional sources empty", func(t *testing.T) {
		c := render(t, nil)
		if c.AMI.On() || c.Binding.ConfigDSN != "" || c.Binding.RegistrationDSN != "" {
			t.Fatalf("%+v %+v", c.AMI, c.Binding)
		}
	})
	t.Run("optional sources given", func(t *testing.T) {
		c := render(t, map[string]string{
			"ASTERISK_CONFIG_DSN": "ro:pw@tcp(localhost:3306)/asterisk", "ASTERISK_REGISTRATION_DSN": "rw:pw@tcp(localhost:3306)/tangra_asterisk",
			"ASTERISK_AMI_ADDRESS": "127.0.0.1:5038", "ASTERISK_AMI_USERNAME": "tangra", "ASTERISK_AMI_SECRET": "s3cret",
		})
		if !c.AMI.On() || c.AMI.Username != "tangra" || c.Binding.ConfigDSN == "" || c.Binding.RegistrationDSN == "" {
			t.Fatalf("%+v %+v", c.AMI, c.Binding)
		}
	})
}
