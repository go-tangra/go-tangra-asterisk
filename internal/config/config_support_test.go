package config

import "testing"

func TestSupportValidation(t *testing.T) {
	base := Default()
	base.Binding.TenantID, base.Binding.PBXID, base.Binding.CDRDSN = "t", "p", "u:p@tcp(h:3306)/cdr"
	base.Gateway.Issuer = "i"
	has := func(c Config, msg string) bool {
		for _, e := range c.ValidateAll() {
			if e.Error() == msg {
				return true
			}
		}
		return false
	}
	if !base.Support.On() || base.Support.CallbackMinutes != 120 || has(base, "support.callback_minutes must be 1 to 1440") {
		t.Fatalf("defaults: %+v", base.Support)
	}
	c := base
	c.Support.CallbackMinutes = 0
	if !has(c, "support.callback_minutes must be 1 to 1440") {
		t.Fatal("zero callback window accepted")
	}
	c = base
	c.Support.Exclude = []string{"201-211", "abc"}
	if !has(c, "support.groups and support.exclude take numbers or number ranges") {
		t.Fatal("non-numeric exclusion accepted")
	}
	off := false
	c.Support.Enabled = &off
	if c.Support.On() {
		t.Fatal("enabled: false ignored")
	}
}
