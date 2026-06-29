package redact

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

var secretPromptPattern = regexp.MustCompile(`(?i)(password|passphrase|verification code|otp|one-time|sudo).*[:：]\s*$`)

// Redactor tracks prompts and redacts input while a secret is expected.
type Redactor struct {
	mu        sync.Mutex
	enabled   bool
	secret    bool
	promptBuf string
	secretLen int
}

// New creates a redactor. If enabled is false, input is returned unchanged.
func New(enabled bool) *Redactor {
	return &Redactor{enabled: enabled}
}

// ObserveOutput updates secret detection state from terminal output.
func (r *Redactor) ObserveOutput(chunk []byte) {
	if r == nil || !r.enabled {
		return
	}

	text := stripVT(string(chunk))
	if text == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.promptBuf += text
	if len(r.promptBuf) > 512 {
		r.promptBuf = r.promptBuf[len(r.promptBuf)-512:]
	}

	lastLine := r.promptBuf
	if idx := strings.LastIndexAny(lastLine, "\r\n"); idx >= 0 {
		lastLine = lastLine[idx+1:]
	}
	if secretPromptPattern.MatchString(lastLine) {
		r.secret = true
		r.secretLen = 0
	}
}

// RedactInput returns the log-safe representation for input.
func (r *Redactor) RedactInput(chunk []byte) string {
	if r == nil || !r.enabled {
		return ""
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.secret {
		return ""
	}

	entered := false
	for _, b := range chunk {
		if b == '\r' || b == '\n' {
			entered = true
			continue
		}
		r.secretLen++
	}

	redacted := fmt.Sprintf("<redacted:%d bytes>", r.secretLen)
	if entered {
		r.secret = false
		r.secretLen = 0
	}
	return redacted
}

func stripVT(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			i++
			if i < len(s) && s[i] == '[' {
				for i < len(s) && (s[i] < '@' || s[i] > '~') {
					i++
				}
			}
			continue
		}
		out.WriteByte(s[i])
	}
	return out.String()
}
