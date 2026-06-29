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
// A small rolling history of recent complete lines is always tracked so
// that CurrentLine() works even when redaction is disabled.
func (r *Redactor) ObserveOutput(chunk []byte) {
	if r == nil {
		return
	}

	text := stripVT(string(chunk))
	if text == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.promptBuf += text
	if len(r.promptBuf) > 4096 {
		r.promptBuf = r.promptBuf[len(r.promptBuf)-4096:]
	}

	// Only do prompt detection if redaction is enabled.
	if r.enabled {
		lastLine := r.promptBuf
		if idx := strings.LastIndexAny(lastLine, "\r\n"); idx >= 0 {
			lastLine = lastLine[idx+1:]
		}
		if secretPromptPattern.MatchString(lastLine) {
			r.secret = true
			r.secretLen = 0
		}
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

// CurrentLine returns the most recent complete visible line from
// recently observed output, with any leading shell prompt prefix removed.
// This is useful for capturing commands that were recalled from shell
// history with arrow keys (the command text appears as PTY output, not
// as keyboard input).  We scan backwards through completed lines so a
// trailing empty prompt (e.g. "❯ ") on the last line is ignored.
func (r *Redactor) CurrentLine() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	lines := splitLines(r.promptBuf)
	r.mu.Unlock()

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if stripped, ok := stripPrompt(line); ok {
			stripped = strings.TrimSpace(stripped)
			if stripped == "" {
				continue
			}
			return stripped
		}
		return line
	}
	return ""
}

// splitLines splits s on CR or LF and returns the segments, dropping
// empty segments so the result contains only completed lines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			if seg := s[start:i]; seg != "" {
				lines = append(lines, seg)
			}
			if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if seg := s[start:]; seg != "" {
		lines = append(lines, seg)
	}
	return lines
}

func stripVT(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b == 0x1b {
			i++
			if i >= len(s) {
				break
			}
			switch s[i] {
			case '[':
				// CSI: skip until a final byte in the range 0x40-0x7E.
				i++
				for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
					i++
				}
			case ']':
				// OSC: skip until BEL (0x07) or ST (ESC \).
				i++
				for i < len(s) && s[i] != 0x07 {
					if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
						i++
						break
					}
					i++
				}
			default:
				// Two-byte escape sequence.
			}
			continue
		}
		// Drop stray control characters that can leak into the prompt buffer
		// (BEL, backspace, escape, etc.).
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' {
			continue
		}
		out.WriteByte(b)
	}
	return out.String()
}

// promptRegex matches common shell prompt prefixes so CurrentLine() can strip
// them from recalled history commands.  The patterns are intentionally broad
// because prompt formatting is user-specific.  Each alternative is anchored
// at the start of the line.
//
//	"❯ ", "➜ ", "… "                — oh-my-zsh / powerline-style prompts
//	"$ ", "# ", "% ", "> "           — classic Bourne / csh prompts
//	"user@host:/path$ "             — Debian default
//	"[user@host path]$ "            — RHEL default
//	"(venv) user@host:~$ "         — virtualenv-wrapped prompts
//	"~/p on main ❯ ", "/var/x on u "— starship path + git branch
//	"╭─ "                           — some starship themes
var promptRegex = regexp.MustCompile(
	`^(\s*(?:` +
		`[❯➜…]\s*` +
		`|[#$%>]\s+` +
		`|\([^)]+\)\s*` +
		`|\[[^\]]+\]\s*` +
		`|\w+@[\w.-]+(?::[^\s$#%>❯➜…]+)?\s*` +
		`|[~/][^❯➜…]*?\s+[❯➜…]\s*` +
		`|╭─\s*` +
		`)+)\s*`,
)

// stripPrompt removes a leading shell prompt from a line.  Returns the
// remaining text and whether anything was stripped.
func stripPrompt(line string) (string, bool) {
	loc := promptRegex.FindStringIndex(line)
	if loc == nil || loc[0] != 0 {
		return line, false
	}
	return line[loc[1]:], true
}
