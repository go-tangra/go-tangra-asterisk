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
