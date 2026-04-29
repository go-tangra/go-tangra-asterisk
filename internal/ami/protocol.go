// Package ami implements a minimal Asterisk Manager Interface client just
// large enough to subscribe to ContactStatus events and persist them.
//
// The AMI wire format is plain TCP with case-insensitive `Key: Value\r\n`
// lines, terminated by a blank line. The first line on connect is a banner
// like `Asterisk Call Manager/9.0.0`. Actions and responses are correlated
// via the optional `ActionID` header.
package ami

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Message is a single AMI frame parsed into a key→value map. AMI keys are
// case-insensitive on the wire; we normalise to canonical form by storing
// keys with their original casing but providing case-insensitive lookup.
type Message struct {
	fields map[string]string
}

func newMessage() *Message { return &Message{fields: make(map[string]string, 8)} }

// Get returns the value for a header (case-insensitive lookup) or "".
func (m *Message) Get(key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m.fields[strings.ToLower(key)]; ok {
		return v
	}
	return ""
}

// Type returns the value of the Event or Response header — whichever is
// present. Empty when neither is set (banner messages).
func (m *Message) Type() string {
	if e := m.Get("Event"); e != "" {
		return e
	}
	return m.Get("Response")
}

// readMessage parses one frame from the stream. A frame ends at a blank
// line. Returns io.EOF when the connection is closed cleanly.
func readMessage(r *bufio.Reader) (*Message, error) {
	msg := newMessage()
	saw := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if saw && err == io.EOF {
				return msg, nil
			}
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if !saw {
				// Empty frame at start of stream — skip.
				continue
			}
			return msg, nil
		}
		saw = true
		// Banner lines like "Asterisk Call Manager/9.0.0" have no colon.
		// Park them under a synthetic "Banner" key so callers can ignore.
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			msg.fields["banner"] = line
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		msg.fields[strings.ToLower(key)] = val
	}
}

// writeAction serialises and sends an action. Headers are written in the
// caller-supplied order so we keep the readable Action/ActionID/... layout.
func writeAction(w io.Writer, headers [][2]string) error {
	var b strings.Builder
	for _, kv := range headers {
		fmt.Fprintf(&b, "%s: %s\r\n", kv[0], kv[1])
	}
	b.WriteString("\r\n")
	_, err := io.WriteString(w, b.String())
	return err
}
