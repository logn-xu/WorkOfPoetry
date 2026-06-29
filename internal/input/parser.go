package input

import (
	"encoding/base64"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Options controls how raw terminal input is represented in audit logs.
type Options struct {
	RawInputLog bool
}

// EventData converts an input byte chunk into structured log data.
func EventData(chunk []byte, redacted string, opts Options) map[string]any {
	data := map[string]any{
		"byte_count": len(chunk),
	}

	keys, mode := Keys(chunk), InputMode(chunk)
	if len(keys) > 0 {
		data["keys"] = keys
	}
	if mode != "" {
		data["mode"] = mode
	}

	if redacted != "" {
		data["text"] = redacted
	} else if mode == "key" {
		if text := decodedText(chunk); text != "" {
			data["text"] = text
		}
	}

	if opts.RawInputLog {
		data["raw_base64"] = base64.StdEncoding.EncodeToString(chunk)
	}

	return data
}

// Keys extracts common key/control semantics from a terminal input chunk.
// Win32 input mode key-release events are skipped.
func Keys(chunk []byte) []string {
	if IsKeyRelease(chunk) {
		return nil
	}
	var keys []string
	for i := 0; i < len(chunk); i++ {
		switch chunk[i] {
		case '\r', '\n':
			keys = append(keys, "Enter")
		case '\t':
			keys = append(keys, "Tab")
		case 0x03:
			keys = append(keys, "Ctrl+C")
		case 0x04:
			keys = append(keys, "Ctrl+D")
		case 0x08, 0x7f:
			keys = append(keys, "Backspace")
		case 0x1b:
			seq, consumed := escapeSequence(chunk[i:])
			keys = append(keys, seq)
			i += consumed - 1
		}
	}
	return keys
}

// IsKeyRelease reports whether chunk is a Win32 input mode key-release sequence
// (ESC [ ... _ with Kd == 0).  These should not be logged.
func IsKeyRelease(chunk []byte) bool {
	if len(chunk) < 6 || chunk[0] != 0x1b || chunk[1] != '[' {
		return false
	}
	end := 2
	for end < len(chunk) {
		b := chunk[end]
		if b >= 0x40 && b <= 0x7e {
			break
		}
		end++
	}
	if end >= len(chunk) || chunk[end] != '_' {
		return false
	}
	rec, ok := parseWin32Input(string(chunk[2:end]))
	return ok && !rec.KeyDown
}

// InputMode returns a coarse classification of the input chunk such as
// "focus", "mouse", or "key" to make JSONL logs easier to filter.
func InputMode(chunk []byte) string {
	for i := 0; i < len(chunk); i++ {
		if chunk[i] != 0x1b {
			continue
		}
		_, mode, consumed := parseCSI(chunk[i:])
		if mode != "" {
			return mode
		}
		i += consumed - 1
	}
	return "key"
}

func escapeSequence(chunk []byte) (string, int) {
	if len(chunk) >= 2 && chunk[1] == 'O' {
		switch {
		case len(chunk) >= 3 && chunk[2] == 'A':
			return "ArrowUp", 3
		case len(chunk) >= 3 && chunk[2] == 'B':
			return "ArrowDown", 3
		case len(chunk) >= 3 && chunk[2] == 'C':
			return "ArrowRight", 3
		case len(chunk) >= 3 && chunk[2] == 'D':
			return "ArrowLeft", 3
		case len(chunk) >= 3 && chunk[2] == 'H':
			return "Home", 3
		case len(chunk) >= 3 && chunk[2] == 'F':
			return "End", 3
		}
	}
	if len(chunk) >= 2 && chunk[1] == '[' {
		name, _, consumed := parseCSI(chunk)
		if name != "" {
			return name, consumed
		}
	}
	return "Escape", 1
}

// parseCSI attempts to decode a CSI-style escape sequence (starting at chunk[0]
// which must be ESC). It returns a human-readable name, an optional mode tag
// ("focus" or "mouse"), and the number of bytes consumed. If the sequence is
// not a recognised CSI variant it returns ("", "", 1) so the caller falls back
// to the generic "Escape" label.
func parseCSI(chunk []byte) (string, string, int) {
	if len(chunk) < 3 || chunk[0] != 0x1b || chunk[1] != '[' {
		return "", "", 1
	}
	// Locate the final byte in the range 0x40-0x7E that terminates the sequence.
	end := 2
	for end < len(chunk) {
		b := chunk[end]
		if b >= 0x40 && b <= 0x7e {
			break
		}
		end++
	}
	if end >= len(chunk) {
		return "", "", 1
	}
	final := chunk[end]
	params := string(chunk[2:end])
	switch final {
	case 'A', 'B', 'C', 'D', 'H', 'F':
		return arrowName(final), "", end + 1
	case 'I':
		return "FocusIn", "focus", end + 1
	case 'O':
		if end == 2 {
			return "FocusOut", "focus", end + 1
		}
	case 'M', 'm':
		if name, ok := mouseName(params, final == 'm'); ok {
			return name, "mouse", end + 1
		}
	case '_':
		// Windows Terminal Win32 input mode serializes KEY_EVENT_RECORD values as
		// `ESC [ Vk ; Sc ; Uc ; Kd ; Cs ; Rc _`. These sequences are keyboard
		// events, not mouse events. See Microsoft Terminal's Win32KeyboardInput
		// parser.
		if name, ok := win32InputName(params); ok {
			return name, "key", end + 1
		}
	case '~':
		if name, ok := tildeName(params); ok {
			return name, "", end + 1
		}
	}
	return "", "", 1
}

func arrowName(b byte) string {
	switch b {
	case 'A':
		return "ArrowUp"
	case 'B':
		return "ArrowDown"
	case 'C':
		return "ArrowRight"
	case 'D':
		return "ArrowLeft"
	case 'H':
		return "Home"
	case 'F':
		return "End"
	}
	return ""
}

// mouseName decodes xterm SGR mouse reports terminated by 'M' (press) or
// 'm' (release), plus the older numeric form when no '<' prefix is present.
// Windows Terminal's '_' terminated Win32 input mode is handled by
// win32InputName, not here.
func mouseName(params string, released bool) (string, bool) {
	parts := strings.Split(params, ";")
	if len(parts) < 3 {
		return "", false
	}
	rawButton := parts[0]
	sgr := strings.HasPrefix(rawButton, "<")
	if sgr {
		rawButton = strings.TrimPrefix(rawButton, "<")
	}
	button, err := strconv.Atoi(rawButton)
	if err != nil {
		return "", false
	}
	base := button
	if !sgr {
		if button < 32 {
			return "", false
		}
		base = button - 32
	}
	if base >= 64 {
		// Wheel events in xterm/SGR encoding.
		switch base {
		case 64:
			return "WheelUp", true
		case 65:
			return "WheelDown", true
		case 66:
			return "WheelLeft", true
		case 67:
			return "WheelRight", true
		}
	}
	btn := []string{"", "Middle", "Right", "None"}[base&0x3]
	if base&0x3 == 0 {
		btn = "Left"
	}
	action := "Press"
	if released {
		action = "Release"
	}
	if btn == "None" {
		if !released {
			return "MouseMove", true
		}
		return "MouseRelease", true
	}
	return action + btn, true
}

// win32InputName decodes Windows Terminal's Win32 input mode keyboard record.
// The parameter list is `Vk;Sc;Uc;Kd;Cs;Rc` terminated by `_`:
//   - Vk: wVirtualKeyCode
//   - Sc: wVirtualScanCode
//   - Uc: UnicodeChar (decimal code point)
//   - Kd: bKeyDown (1 press, 0 release)
//   - Cs: dwControlKeyState
//   - Rc: wRepeatCount
func win32InputName(params string) (string, bool) {
	rec, ok := parseWin32Input(params)
	if !ok {
		return "", false
	}
	label := win32KeyLabel(rec)
	if !rec.KeyDown {
		return "Release" + label, true
	}
	return label, true
}

type win32InputRecord struct {
	VK        int
	ScanCode  int
	Unicode   int
	KeyDown   bool
	CtrlState int
	Repeat    int
}

func parseWin32Input(params string) (win32InputRecord, bool) {
	parts := strings.Split(params, ";")
	if len(parts) < 6 {
		return win32InputRecord{}, false
	}
	values := make([]int, 6)
	for i := range values {
		v, err := strconv.Atoi(parts[i])
		if err != nil {
			return win32InputRecord{}, false
		}
		values[i] = v
	}
	return win32InputRecord{
		VK:        values[0],
		ScanCode:  values[1],
		Unicode:   values[2],
		KeyDown:   values[3] != 0,
		CtrlState: values[4],
		Repeat:    values[5],
	}, true
}

func win32KeyLabel(rec win32InputRecord) string {
	if rec.CtrlState&(leftCtrlPressed|rightCtrlPressed) != 0 && rec.Unicode >= 1 && rec.Unicode <= 26 {
		return "Ctrl+" + string(rune('A'+rec.Unicode-1))
	}
	switch rec.VK {
	case 0x08:
		return "Backspace"
	case 0x09:
		return "Tab"
	case 0x0d:
		return "Enter"
	case 0x10:
		return "Shift"
	case 0x11:
		return "Ctrl"
	case 0x12:
		return "Alt"
	case 0x14:
		return "CapsLock"
	case 0x1b:
		return "Escape"
	case 0x21:
		return "PageUp"
	case 0x22:
		return "PageDown"
	case 0x23:
		return "End"
	case 0x24:
		return "Home"
	case 0x25:
		return "ArrowLeft"
	case 0x26:
		return "ArrowUp"
	case 0x27:
		return "ArrowRight"
	case 0x28:
		return "ArrowDown"
	case 0x2d:
		return "Insert"
	case 0x2e:
		return "Delete"
	}
	if rec.VK >= 0x70 && rec.VK <= 0x7b {
		return "F" + strconv.Itoa(rec.VK-0x6f)
	}
	if rec.Unicode >= 0x20 && rec.Unicode != 0x7f {
		return string(rune(rec.Unicode))
	}
	if rec.VK >= 'A' && rec.VK <= 'Z' {
		return string(rune(rec.VK))
	}
	if rec.VK >= '0' && rec.VK <= '9' {
		return string(rune(rec.VK))
	}
	return "VK_" + strconv.Itoa(rec.VK)
}

const (
	rightAltPressed  = 0x0001
	leftAltPressed   = 0x0002
	rightCtrlPressed = 0x0004
	leftCtrlPressed  = 0x0008
)

// tildeName decodes a CSI <n> ~ sequence (Insert/Delete/PageUp/PageDown/F1-F12).
func tildeName(params string) (string, bool) {
	n, err := strconv.Atoi(params)
	if err != nil {
		return "", false
	}
	switch n {
	case 1, 7:
		return "Home", true
	case 2, 8:
		return "End", true
	case 3:
		return "Delete", true
	case 5:
		return "PageUp", true
	case 6:
		return "PageDown", true
	case 11, 12, 13, 14, 15:
		return "F" + strconv.Itoa(n-10), true
	case 17, 18, 19, 20, 21:
		return "F" + strconv.Itoa(n-7), true
	case 23, 24:
		return "F" + strconv.Itoa(n-16), true
	case 25, 26, 27, 28, 29, 30, 31, 32, 33, 34:
		return "F" + strconv.Itoa(n-1), true
	}
	return "", false
}

func decodedText(chunk []byte) string {
	var out strings.Builder
	sawWin32 := false
	for i := 0; i < len(chunk); i++ {
		if chunk[i] != 0x1b || i+2 >= len(chunk) || chunk[i+1] != '[' {
			continue
		}
		end := i + 2
		for end < len(chunk) {
			b := chunk[end]
			if b >= 0x40 && b <= 0x7e {
				break
			}
			end++
		}
		if end >= len(chunk) || chunk[end] != '_' {
			continue
		}
		rec, ok := parseWin32Input(string(chunk[i+2 : end]))
		if !ok {
			continue
		}
		sawWin32 = true
		if rec.KeyDown && rec.Unicode >= 0x20 && rec.Unicode != 0x7f {
			repeat := rec.Repeat
			if repeat <= 0 {
				repeat = 1
			}
			for range repeat {
				out.WriteRune(rune(rec.Unicode))
			}
		}
		i = end
	}
	if sawWin32 {
		return out.String()
	}
	return printableText(chunk)
}

func printableText(chunk []byte) string {
	if len(chunk) == 0 || chunk[0] == 0x1b {
		return ""
	}
	if !utf8.Valid(chunk) {
		return ""
	}
	text := string(chunk)
	text = strings.ReplaceAll(text, "\r", "")
	text = strings.ReplaceAll(text, "\n", "")
	text = strings.ReplaceAll(text, "\t", "")
	text = strings.TrimFunc(text, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	})
	if strings.ContainsRune(text, '\x1b') {
		return ""
	}
	return text
}
