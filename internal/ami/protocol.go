package ami

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

func ReadFrame(r *bufio.Reader) (map[string]string, error) {
	m := map[string]string{}
	size := 0
	for {
		var raw []byte
		for {
			piece, e := r.ReadSlice('\n')
			size += len(piece)
			if size > 65536 {
				return nil, errors.New("AMI frame exceeds bound")
			}
			raw = append(raw, piece...)
			if e == bufio.ErrBufferFull {
				continue
			}
			if e != nil {
				return nil, e
			}
			break
		}
		line := string(raw)

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(m) > 0 {
				return m, nil
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if ok {
			m[k] = strings.TrimSpace(v)
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
