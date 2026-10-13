package exporter

import (
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/calls"
	"github.com/go-tangra/go-tangra/v4/observe"
	"net/http/httptest"
	"strings"
	"testing"
)

// The collector's gauges reach the admin /metrics handler; the Monitoring
// dashboards query them through Prometheus (framework < 4.9.2 dropped gauges).
func TestGaugesRenderedOnAdminMetrics(t *testing.T) {
	m, e := observe.NewMetrics()
	if e != nil {
		t.Fatal(e)
	}
	r := calls.New()
	r.Status(true) // PBX gauges are reported only while the AMI view is fresh
	c := New(r)
	c.BeginSweep()
	c.Frame(map[string]string{"ActionID": "metrics-CoreStatus", "Response": "Success", "CoreCurrentCalls": "4"})
	if _, e = c.Register(m.Meter("asterisk")); e != nil {
		t.Fatal(e)
	}
	if _, e = c.RegisterPBX(m.Meter("asterisk")); e != nil {
		t.Fatal(e)
	}
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{"# TYPE asterisk_up gauge\n", "asterisk_up 1\n", "asterisk_active_calls 0\n", "asterisk_channels_active 0\n", "asterisk_current_calls 4\n"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}
