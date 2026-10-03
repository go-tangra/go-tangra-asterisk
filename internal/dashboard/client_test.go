package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNormalizationBoundsAndIsolation(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"instance":"pbx"},"value":[100,"NaN"]}]}}`))
	}))
	defer s.Close()
	c, e := New(s.URL, true, "t", "p")
	if e != nil {
		t.Fatal(e)
	}
	v, e := c.Instant(context.Background(), "t", "up", time.Now())
	if e != nil || len(v) != 1 || v[0].Samples[0].HasValue {
		t.Fatalf("bad normalization: %v %v", v, e)
	}
	if _, e = c.Instant(context.Background(), "foreign", "up", time.Now()); e == nil {
		t.Fatal("foreign tenant allowed")
	}
	if _, e = c.Range(context.Background(), "t", "up", time.Now(), time.Now().Add(time.Hour), 0); e == nil {
		t.Fatal("zero step")
	}
	if _, e = New(s.URL, false, "t", "p"); e == nil {
		t.Fatal("shared upstream allowed")
	}
}

func TestRangeMissingValuesAndForeignLabels(t *testing.T) {
	foreign := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if foreign {
			w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"tenant_id":"foreign"},"value":[100,"1"]}]}}`))
			return
		}
		if r.URL.Query().Get("step") != "60" {
			t.Error("validated step missing")
		}
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[100,"1.5"],[160,"+Inf"],[220,"NaN"]]}]}}`))
	}))
	defer server.Close()
	client, _ := New(server.URL, true, "t", "p")
	start := time.Unix(100, 0)
	v, e := client.Range(context.Background(), "t", "up", start, start.Add(120*time.Second), 60)
	if e != nil || len(v) != 1 || len(v[0].Samples) != 3 || !v[0].Samples[0].HasValue || v[0].Samples[1].HasValue || v[0].Samples[2].Value != nil {
		t.Fatalf("bad range: %+v %v", v, e)
	}
	foreign = true
	if _, e = client.Instant(context.Background(), "t", "up", start); e == nil {
		t.Fatal("foreign metric labels returned")
	}
	if _, e = client.Range(context.Background(), "t", "up", start, start.Add(365*24*time.Hour), 60); e == nil {
		t.Fatal("oversized period accepted")
	}
}
