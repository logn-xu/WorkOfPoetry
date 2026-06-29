package term

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
	xterm "golang.org/x/term"
)

const enableVirtualTerminalProcessing uint32 = 0x0004

// RawState stores terminal state restored after raw mode exits.
type RawState struct {
	fd    int
	state *xterm.State
}

// MakeRaw puts stdin into raw mode.
func MakeRaw(file *os.File) (*RawState, error) {
	fd := int(file.Fd())
	state, err := xterm.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("enable raw mode: %w", err)
	}
	return &RawState{fd: fd, state: state}, nil
}

// Restore restores terminal state.
func (s *RawState) Restore() error {
	if s == nil || s.state == nil {
		return nil
	}
	return xterm.Restore(s.fd, s.state)
}

// Size returns terminal width and height, falling back to 80x25.
func Size(file *os.File) (int, int) {
	w, h, err := xterm.GetSize(int(file.Fd()))
	if err != nil || w <= 0 || h <= 0 {
		return 80, 25
	}
	return w, h
}

// EnableVT enables virtual terminal sequence processing for stdout where supported.
func EnableVT(file *os.File) error {
	handle := windows.Handle(file.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return nil
	}
	mode |= enableVirtualTerminalProcessing
	if err := windows.SetConsoleMode(handle, mode); err != nil {
		return fmt.Errorf("enable virtual terminal processing: %w", err)
	}
	return nil
}
