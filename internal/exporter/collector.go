package exporter

import (
	"context"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/calls"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/cdr"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/registration"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"sync"
	"time"
)

type Collector struct {
	Registry       *calls.Registry
	mu             sync.Mutex
	contacts       map[string]registration.Event
	quality        map[string]*cdr.RTPQoS
	pbx            map[string][]point
	pending        map[string][]map[string]string
	sweepStart     time.Time
	SourceLocation *time.Location
}

func New(r *calls.Registry) *Collector {
	return &Collector{Registry: r, contacts: map[string]registration.Event{}, quality: map[string]*cdr.RTPQoS{}, pbx: map[string][]point{}, pending: map[string][]map[string]string{}}
}
func (c *Collector) Contact(e registration.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.contacts) < 10000 || c.contacts[e.Endpoint+e.Contact].Endpoint != "" {
		c.contacts[e.Endpoint+e.Contact] = e
	}
}
func (c *Collector) Register(m metric.Meter) (metric.Registration, error) {
	active, e := m.Int64ObservableGauge("asterisk_active_calls")
	if e != nil {
		return nil, e
	}
	fresh, e := m.Int64ObservableGauge("asterisk_ami_fresh")
	if e != nil {
		return nil, e
	}
	contacts, e := m.Int64ObservableGauge("asterisk_contact_registered")
	if e != nil {
		return nil, e
	}
	rtt, e := m.Float64ObservableGauge("asterisk_contact_rtt_milliseconds")
	if e != nil {
		return nil, e
	}
	rx, e := m.Float64ObservableGauge("asterisk_rtp_receive_mos")
	if e != nil {
		return nil, e
	}
	tx, e := m.Float64ObservableGauge("asterisk_rtp_transmit_mos")
	if e != nil {
		return nil, e
	}
	jitter, e := m.Float64ObservableGauge("asterisk_rtp_receive_jitter_milliseconds")
	if e != nil {
		return nil, e
	}
	loss, e := m.Float64ObservableGauge("asterisk_rtp_receive_loss_percent")
	if e != nil {
		return nil, e
	}
	return m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		s := c.Registry.Snapshot()
		o.ObserveInt64(active, int64(len(s.Calls)))
		n := int64(0)
		if s.Fresh {
			n = 1
		}
		o.ObserveInt64(fresh, n)
		c.mu.Lock()
		defer c.mu.Unlock()
		now := time.Now()
		for _, e := range c.contacts {
			status := registration.Evaluate(e.Endpoint, now, []registration.Event{e}, nil)
			attrs := metric.WithAttributes(attribute.String("extension", e.Endpoint), attribute.String("contact", e.Contact))
			n := int64(0)
			if status.Registered {
				n = 1
			}
			o.ObserveInt64(contacts, n, attrs)
			if e.RTT > 0 {
				o.ObserveFloat64(rtt, float64(e.RTT)/1000, attrs)
			}
		}
		for label, q := range c.quality {
			attrs := metric.WithAttributes(attribute.String("extension", label))
			if q.RxMOS != nil {
				o.ObserveFloat64(rx, *q.RxMOS, attrs)
			}
			if q.TxMOS != nil {
				o.ObserveFloat64(tx, *q.TxMOS, attrs)
			}
			if q.RxJitterMs != nil {
				o.ObserveFloat64(jitter, *q.RxJitterMs, attrs)
			}
			if q.RxLossPercent != nil {
				o.ObserveFloat64(loss, *q.RxLossPercent, attrs)
			}
		}
		return nil
	}, active, fresh, contacts, rtt, rx, tx, jitter, loss)
}

func (c *Collector) CollectQuality(ctx context.Context, r *cdr.Repository) {
	for ctx.Err() == nil {
		legs, e := r.RecentQuality(ctx, time.Now().Add(-5*time.Minute))
		q := map[string]*cdr.RTPQoS{}
		if e == nil {
			for _, leg := range legs {
				ext := cdr.ExtractExtension(leg.DstChannel)
				if ext == "" {
					ext = cdr.ExtractExtension(leg.Channel)
				}
				if ext != "" && leg.LocalQuality != nil {
					q[ext] = leg.LocalQuality
				}
			}
		}
		c.mu.Lock()
		c.quality = q
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
}
