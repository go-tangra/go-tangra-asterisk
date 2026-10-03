package cdr

import (
	"context"
	"time"
)

// RecentQuality reads only available optional columns; the bound prevents a
// metrics scrape worker from scanning the whole history database.
func (r *Repository) RecentQuality(ctx context.Context, since time.Time) ([]Leg, error) {
	if !r.Pools.Columns["rtpqos"] && !r.Pools.Columns["peerrtpqos"] {
		return []Leg{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.Pools.Timeout)
	defer cancel()
	return r.legs(ctx, "calldate>=? AND ("+qualityPredicate(r.Pools.Columns)+")", since.In(r.sourceLocation()))
}
func qualityPredicate(cols map[string]bool) string {
	a := ""
	if cols["rtpqos"] {
		a = "COALESCE(rtpqos,'')<>''"
	}
	if cols["peerrtpqos"] {
		if a != "" {
			a += " OR "
		}
		a += "COALESCE(peerrtpqos,'')<>''"
	}
	return a
}
