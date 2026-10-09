package httpapi

import (
	"log/slog"
	"sync"
	"time"
)

// refusalLogEvery bounds how often one refusal reason is logged.
const refusalLogEvery = time.Minute

// refusals logs why operator requests are refused (the verifier's reason,
// at most once a minute per reason and detail; never the token). Without it
// a misconfiguration such as a wrong gateway.issuer is only a silent 401
// behind "Your session has ended".
type refusals struct {
	log *slog.Logger
	now func() time.Time

	mu     sync.Mutex
	logged map[string]time.Time
}

func (f *refusals) refused(reason string, err error) {
	if f == nil || f.log == nil {
		return
	}
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	key := reason + "\x00" + detail
	f.mu.Lock()
	now := time.Now()
	if f.now != nil {
		now = f.now()
	}
	if last, ok := f.logged[key]; ok && now.Sub(last) < refusalLogEvery {
		f.mu.Unlock()
		return
	}
	if f.logged == nil || len(f.logged) > 1024 {
		f.logged = map[string]time.Time{}
	}
	f.logged[key] = now
	f.mu.Unlock()
	f.log.Warn("operator request refused", "reason", reason, "detail", detail)
}
