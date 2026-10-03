package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid monitoring bounds")
var ErrUnavailable = errors.New("monitoring unavailable")

type Sample struct {
	Time     time.Time `json:"time"`
	Value    *float64  `json:"value"`
	HasValue bool      `json:"hasValue"`
}
type Series struct {
	Labels  map[string]string `json:"labels"`
	Samples []Sample          `json:"samples"`
}
type Client struct {
	base        string
	tenant, pbx string
	http        *http.Client
}

func New(base string, dedicated bool, tenant, pbx string) (*Client, error) {
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !dedicated || tenant == "" || pbx == "" {
		return nil, errors.New("dedicated tenant/PBX upstream required")
	}
	return &Client{base: strings.TrimRight(base, "/"), tenant: tenant, pbx: pbx, http: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("upstream redirect refused") }}}, nil
}
func (c *Client) Instant(ctx context.Context, tenant, query string, at time.Time) ([]Series, error) {
	return c.fetch(ctx, tenant, "query", url.Values{"query": {query}, "time": {strconv.FormatFloat(float64(at.UnixMilli())/1000, 'f', 3, 64)}})
}
func (c *Client) Range(ctx context.Context, tenant, query string, start, end time.Time, step int) ([]Series, error) {
	if step < 1 || !end.After(start) || end.Sub(start) > 31*24*time.Hour || end.Sub(start)/time.Second/time.Duration(step) > 10000 {
		return nil, ErrInvalid
	}
	return c.fetch(ctx, tenant, "query_range", url.Values{"query": {query}, "start": {strconv.FormatInt(start.Unix(), 10)}, "end": {strconv.FormatInt(end.Unix(), 10)}, "step": {strconv.Itoa(step)}})
}
func (c *Client) fetch(ctx context.Context, tenant, path string, params url.Values) ([]Series, error) {
	if c == nil {
		return nil, ErrUnavailable
	}
	if tenant != c.tenant {
		return nil, errors.New("monitoring tenant denied")
	}
	if len(params.Get("query")) == 0 || len(params.Get("query")) > 4096 {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", c.base+"/api/v1/"+path+"?"+params.Encode(), nil)
	if e != nil {
		return nil, ErrUnavailable
	}
	res, e := c.http.Do(req)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, ErrUnavailable
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, 4<<20+1))
	if e != nil || len(raw) > 4<<20 {
		return nil, ErrUnavailable
	}
	var body struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string   `json:"metric"`
				Value  []json.RawMessage   `json:"value"`
				Values [][]json.RawMessage `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Status != "success" || (body.Data.ResultType != "vector" && body.Data.ResultType != "matrix") {
		return nil, ErrUnavailable
	}
	out := []Series{}
	count := 0
	for _, r := range body.Data.Result {
		if v := r.Metric["tenant_id"]; v != "" && v != c.tenant {
			return nil, ErrUnavailable
		}
		if v := r.Metric["pbx_id"]; v != "" && v != c.pbx {
			return nil, ErrUnavailable
		}
		s := Series{Labels: r.Metric, Samples: []Sample{}}
		values := r.Values
		if len(r.Value) > 0 {
			values = [][]json.RawMessage{r.Value}
		}
		for _, pair := range values {
			count++
			if count > 100000 || len(pair) != 2 {
				return nil, ErrUnavailable
			}
			var ts float64
			var value string
			if json.Unmarshal(pair[0], &ts) != nil || json.Unmarshal(pair[1], &value) != nil || math.IsNaN(ts) || math.IsInf(ts, 0) {
				return nil, ErrUnavailable
			}
			sample := Sample{Time: time.UnixMilli(int64(ts * 1000)).UTC()}
			v, e := strconv.ParseFloat(value, 64)
			if e == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
				sample.Value = &v
				sample.HasValue = true
			}
			s.Samples = append(s.Samples, sample)
		}
		out = append(out, s)
	}
	return out, nil
}
