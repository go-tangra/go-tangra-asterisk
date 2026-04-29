package data

import "strings"

// ExtractExtension pulls the extension number out of a channel name like
// "PJSIP/45-0000572a", "SIP/100-abcd", or "Local/600@from-internal-00001".
// Returns "" if the channel doesn't look extension-shaped.
func ExtractExtension(channel string) string {
	if channel == "" {
		return ""
	}
	slash := strings.IndexByte(channel, '/')
	if slash < 0 || slash == len(channel)-1 {
		return ""
	}
	tail := channel[slash+1:]
	if i := strings.IndexByte(tail, '@'); i >= 0 {
		tail = tail[:i]
	}
	if i := strings.IndexByte(tail, '-'); i >= 0 {
		tail = tail[:i]
	}
	if !looksLikeExtension(tail) {
		return ""
	}
	return tail
}

// looksLikeExtension returns true when s contains at least one digit and no
// whitespace — extensions in FreePBX are short numeric or alphanumeric ids
// (e.g. "45", "600", "agent01"), but never random PJSIP trunk noise like
// "ITD" alone (no digits).
func looksLikeExtension(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	hasDigit := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			hasDigit = true
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' {
			continue
		}
		return false
	}
	return hasDigit
}
