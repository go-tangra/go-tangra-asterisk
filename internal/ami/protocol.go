package ami

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// FrameReader reads AMI frames. A read error (a deadline expiring mid-frame)
// keeps the partial line and fields, so the next Next resumes the frame.
type FrameReader struct {
	r    *bufio.Reader
	m    map[string]string
	raw  []byte
	size int
}

func NewFrameReader(r *bufio.Reader) *FrameReader { return &FrameReader{r: r} }

func ReadFrame(r *bufio.Reader) (map[string]string, error) { return NewFrameReader(r).Next() }

func (f *FrameReader) Next() (map[string]string, error) {
	if f.m == nil {
		f.m = map[string]string{}
	}
	for {
		for {
			piece, e := f.r.ReadSlice('\n')
			f.size += len(piece)
			if f.size > 65536 {
				f.m, f.raw, f.size = nil, nil, 0
				return nil, errors.New("AMI frame exceeds bound")
			}
			f.raw = append(f.raw, piece...)
			if e == bufio.ErrBufferFull {
				continue
			}
			if e != nil {
				return nil, e
			}
			break
		}
		line := strings.TrimRight(string(f.raw), "\r\n")
		f.raw = f.raw[:0]
		if line == "" {
			if len(f.m) > 0 {
				m := f.m
				f.m, f.size = nil, 0
				return m, nil
			}
			f.size = 0
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if ok {
			f.m[k] = strings.TrimSpace(v)
		}
	}
}
func WriteAction(w io.Writer, action string, fields map[string]string) error {
	allowed := map[string]bool{"Login": true, "Logoff": true, "PJSIPShowContacts": true, "CoreShowChannels": true, "Ping": true, "CoreSettings": true, "CoreStatus": true, "PJSIPShowEndpoints": true, "SIPpeers": true, "QueueStatus": true}
	if !allowed[action] {
		return errors.New("AMI control action denied")
	}
	s := "Action: " + action + "\r\n"
	for k, v := range fields {
		if strings.ContainsAny(k+v, "\r\n") {
			return errors.New("invalid AMI field")
		}
		s += k + ": " + v + "\r\n"
	}
	_, e := io.WriteString(w, s+"\r\n")
	return e
}
