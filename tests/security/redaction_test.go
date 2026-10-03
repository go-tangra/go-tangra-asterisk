package security_test

import (
	"encoding/json"
	"fmt"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/config"
	"strings"
	"testing"
)

func TestConfigSecretsRedacted(t *testing.T) {
	c := config.Default()
	c.Binding.CDRDSN = "user:verysecret@tcp(localhost)/cdr"
	c.Binding.MonitoringURL = "http://sensitive"
	c.AMI.Secret = "amipassword"
	raw, _ := json.Marshal(c)
	for _, s := range []string{string(raw), fmt.Sprintf("%v", c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c)} {
		for _, secret := range []string{"verysecret", "amipassword", "sensitive"} {
			if strings.Contains(s, secret) {
				t.Fatal("secret in serialized config")
			}
		}
	}
}
