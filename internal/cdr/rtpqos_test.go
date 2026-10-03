package cdr

import "testing"

func TestQualityAbsentMalformedAndDirectional(t *testing.T) {
	for _, s := range []string{"", "garbage", "rxmes=NaN", "rxmes=abc", "lp=-2"} {
		if ParseRTPQoS(s) != nil {
			t.Fatalf("accepted %q", s)
		}
	}
	q := ParseRTPQoS("rxmes=90;txmes=40;rxjitter=0.002;lp=1;rxcount=99")
	if q == nil || q.RxJitterMs == nil || *q.RxJitterMs != 2 || q.Quality != "BAD" || q.RxLossPercent == nil || *q.RxLossPercent != 1 {
		t.Fatalf("bad quality: %+v", q)
	}
}

func TestZeroMESIsNoReport(t *testing.T) {
	q := ParseRTPQoS("rxmes=0;txmes=0")
	if q == nil || q.RxMOS != nil || q.TxMOS != nil || q.Quality != "" {
		t.Fatalf("zero RTCP report fabricated quality: %+v", q)
	}
}
