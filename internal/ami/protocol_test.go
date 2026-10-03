package ami

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestFramesAndBounds(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("Asterisk Call Manager/5.0\r\nEvent: Newchannel\r\nUniqueid: a\r\n\r\n"))
	m, e := ReadFrame(r)
	if e != nil || m["Event"] != "Newchannel" {
		t.Fatalf("%v %v", m, e)
	}
	if _, e = ReadFrame(bufio.NewReader(strings.NewReader(strings.Repeat("x", 70000)))); e == nil {
		t.Fatal("oversized frame accepted")
	}
}

func TestFrameResumesAfterReadError(t *testing.T) {
	pr, pw := io.Pipe()
	f := NewFrameReader(bufio.NewReader(&flaky{r: pr}))
	go func() {
		io.WriteString(pw, "Event: Hangup\r\nUnique")
		io.WriteString(pw, "id: a\r\n\r\n")
	}()
	if _, e := f.Next(); e != errTimeout {
		t.Fatalf("first read: %v", e)
	}
	m, e := f.Next()
	if e != nil || m["Event"] != "Hangup" || m["Uniqueid"] != "a" {
		t.Fatalf("%v %v", m, e)
	}
}

var errTimeout = errors.New("timeout")

// flaky fails every second read, as an expired deadline does mid-frame.
type flaky struct {
	r io.Reader
	n int
}

func (f *flaky) Read(p []byte) (int, error) {
	f.n++
	if f.n == 2 {
		return 0, errTimeout
	}
	return f.r.Read(p)
}
