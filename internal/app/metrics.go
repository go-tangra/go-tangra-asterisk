package app

import "github.com/go-tangra/go-tangra-asterisk/v4/internal/exporter"

func (a *App) metrics(c *exporter.Collector) error {
	_, e := c.Register(a.Freya.Metrics().Meter("asterisk"))
	if e != nil {
		return e
	}
	_, e = c.RegisterPBX(a.Freya.Metrics().Meter("asterisk"))
	return e
}
