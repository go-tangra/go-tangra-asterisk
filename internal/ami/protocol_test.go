package ami

import (
	"bufio"
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
