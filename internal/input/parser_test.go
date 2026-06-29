package input

import "testing"

func TestKeysWin32InputMode(t *testing.T) {
	cases := []struct {
		raw  string
		want string // empty means no keys expected
	}{
		// Windows Terminal Win32 input mode reports: Vk;Sc;Uc;Kd;Cs;Rc _.
		{"\x1b[80;25;112;1;32;1_", "p"},
		{"\x1b[80;25;112;0;32;1_", ""}, // release — filtered
		{"\x1b[13;28;13;1;32;1_", "Enter"},
		{"\x1b[13;28;13;0;32;1_", ""}, // release Enter — filtered
		{"\x1b[87;17;119;1;32;1_", "w"},
		{"\x1b[68;32;100;1;32;1_", "d"},
		{"\x1b[9;15;9;1;32;1_", "Tab"},
		{"\x1b[17;29;0;1;40;1_", "Ctrl"},
		{"\x1b[67;46;3;1;40;1_", "Ctrl+C"},
		{"\x1b[68;32;4;1;40;1_", "Ctrl+D"},
		// Focus events.
		{"\x1b[I", "FocusIn"},
		{"\x1b[O", "FocusOut"},
	}
	for _, tc := range cases {
		keys := Keys([]byte(tc.raw))
		if tc.want == "" {
			if len(keys) != 0 {
				t.Errorf("Keys(%q) = %v, want []", tc.raw, keys)
			}
			continue
		}
		if len(keys) != 1 || keys[0] != tc.want {
			t.Errorf("Keys(%q) = %v, want [%s]", tc.raw, keys, tc.want)
		}
	}
}

func TestIsKeyRelease(t *testing.T) {
	tests := []struct {
		raw     string
		release bool
	}{
		{"\x1b[80;25;112;1;32;1_", false},
		{"\x1b[80;25;112;0;32;1_", true},
		{"\x1b[13;28;13;0;32;1_", true},
		{"abc", false},
		{"\x1b[I", false},
	}
	for _, tc := range tests {
		if got := IsKeyRelease([]byte(tc.raw)); got != tc.release {
			t.Errorf("IsKeyRelease(%q) = %v, want %v", tc.raw, got, tc.release)
		}
	}
}

func TestKeysPlainKeys(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"abc", ""},
		{"\r", "Enter"},
		{"\n", "Enter"},
		{"\t", "Tab"},
		{"\x03", "Ctrl+C"},
		{"\x04", "Ctrl+D"},
		{"\x7f", "Backspace"},
		{"\x1b[A", "ArrowUp"},
		{"\x1b[B", "ArrowDown"},
		{"\x1b[C", "ArrowRight"},
		{"\x1b[D", "ArrowLeft"},
		{"\x1b[H", "Home"},
		{"\x1b[F", "End"},
		{"\x1b[3~", "Delete"},
		{"\x1b", "Escape"},
	}
	for _, tc := range cases {
		keys := Keys([]byte(tc.raw))
		if tc.want == "" {
			if len(keys) != 0 {
				t.Errorf("Keys(%q) = %v, want []", tc.raw, keys)
			}
			continue
		}
		if len(keys) != 1 || keys[0] != tc.want {
			t.Errorf("Keys(%q) = %v, want [%s]", tc.raw, keys, tc.want)
		}
	}
}

func TestInputMode(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"abc", "key"},
		{"\x1b[I", "focus"},
		{"\x1b[O", "focus"},
		{"\x1b[80;25;112;1;32;1_", "key"},
		{"\x1b[<0;10;20M", "mouse"},
		{"\r", "key"},
	}
	for _, tc := range cases {
		if got := InputMode([]byte(tc.raw)); got != tc.want {
			t.Errorf("InputMode(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestEventDataWin32Input(t *testing.T) {
	data := EventData([]byte("\x1b[80;25;112;1;32;1_"), "", Options{})
	if data["mode"] != "key" {
		t.Errorf("mode = %v, want key", data["mode"])
	}
	keys, _ := data["keys"].([]string)
	if len(keys) != 1 || keys[0] != "p" {
		t.Errorf("keys = %v, want [p]", data["keys"])
	}
	if data["text"] != "p" {
		t.Errorf("text = %v, want p", data["text"])
	}
}

func TestEventDataMouse(t *testing.T) {
	data := EventData([]byte("\x1b[<0;10;20M"), "", Options{})
	if data["mode"] != "mouse" {
		t.Errorf("mode = %v, want mouse", data["mode"])
	}
	keys, _ := data["keys"].([]string)
	if len(keys) != 1 || keys[0] != "PressLeft" {
		t.Errorf("keys = %v, want [PressLeft]", data["keys"])
	}
}

func TestEventDataFocus(t *testing.T) {
	data := EventData([]byte("\x1b[I"), "", Options{})
	if data["mode"] != "focus" {
		t.Errorf("mode = %v, want focus", data["mode"])
	}
	if _, ok := data["text"]; ok {
		t.Errorf("text should not be present for focus event, got %v", data["text"])
	}
}

func TestEventDataReleaseNotLogged(t *testing.T) {
	data := EventData([]byte("\x1b[80;25;112;0;32;1_"), "", Options{})
	if _, ok := data["keys"]; ok {
		t.Errorf("keys should not be present for release event, got %v", data["keys"])
	}
	if _, ok := data["text"]; ok {
		t.Errorf("text should not be present for release event, got %v", data["text"])
	}
}
