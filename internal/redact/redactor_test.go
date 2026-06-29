package redact

import "testing"

func TestStripVT(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		// CSI cursor positioning from the log sample: ESC[5;3H.
		{"\x1b[5;3H", ""},
		// CSI SGR + OSC title.
		{"\x1b[0;37;40m\x1b]0;title\x07", ""},
		// Plain text plus a stray BEL.
		{"hello\x07world", "helloworld"},
		// Cursor save/restore: ESC7 / ESC8.
		{"\x1b7hi\x1b8", "hi"},
		// Bell at start.
		{"\x07ls", "ls"},
		// Newlines preserved.
		{"a\nb", "a\nb"},
	}
	for _, tc := range cases {
		if got := stripVT(tc.raw); got != tc.want {
			t.Errorf("stripVT(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestStripPrompt(t *testing.T) {
	cases := []struct {
		raw      string
		want     string
		stripped bool
	}{
		// oh-my-zsh style.
		{"❯ ls", "ls", true},
		{"❯  sudo su -", "sudo su -", true},
		{"➜ ls -la", "ls -la", true},
		// Powerline starship.
		{"~/p workofpoetry on main ❯ ls", "ls", true},
		// Bash classics.
		{"$ ls", "ls", true},
		{"# apt update", "apt update", true},
		{"% csh -V", "csh -V", true},
		// user@host:path$.
		{"user@host:/home/x$ ls", "ls", true},
		{"[user@host /home/x]$ ls", "ls", true},
		// Virtualenv wrapper.
		{"(venv) user@host:~$ python", "python", true},
		// Lines that are not prompts.
		{"hello world", "hello world", false},
		{"ls -la", "ls -la", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := stripPrompt(tc.raw)
		if ok != tc.stripped {
			t.Errorf("stripPrompt(%q) stripped = %v, want %v", tc.raw, ok, tc.stripped)
		}
		if got != tc.want {
			t.Errorf("stripPrompt(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestRedactorCurrentLineStripsPrompt(t *testing.T) {
	r := New(true)
	// Simulate a shell printing the previous command and a new prompt.
	// `❯ ls github.com/logn-xu/WorkOfPoetry\r\n` is the recalled command,
	// followed by a fresh prompt `❯ ` (the visible "current line" once
	// arrow-up has repainted the screen).
	r.ObserveOutput([]byte("\x1b[5;3H\x1b[0;37;40m❯ ls github.com/logn-xu/WorkOfPoetry\r\n❯ "))
	if got := r.CurrentLine(); got != "ls github.com/logn-xu/WorkOfPoetry" {
		t.Errorf("CurrentLine = %q, want %q", got, "ls github.com/logn-xu/WorkOfPoetry")
	}
}
