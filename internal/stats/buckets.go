package stats

import "time"

func BucketStart(t time.Time, loc *time.Location, bucket string) time.Time {
	l := t.In(loc)
	if bucket == "hour" {
		return l.Add(-time.Duration(l.Minute())*time.Minute - time.Duration(l.Second())*time.Second - time.Duration(l.Nanosecond())).UTC()
	}
	d := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
	if bucket == "week" {
		days := (int(d.Weekday()) + 6) % 7
		d = d.AddDate(0, 0, -days)
	}
	return d.UTC()
}
