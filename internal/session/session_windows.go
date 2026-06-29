//go:build windows

package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/charmbracelet/x/conpty"
	"github.com/logn-xu/WorkOfPoetry/internal/input"
	"github.com/logn-xu/WorkOfPoetry/internal/logging"
	"github.com/logn-xu/WorkOfPoetry/internal/redact"
	termutil "github.com/logn-xu/WorkOfPoetry/internal/term"
	"golang.org/x/sys/windows"
)

// Run starts ssh.exe inside ConPTY and bridges terminal I/O.
func Run(ctx context.Context, cfg Config) (int, error) {
	if cfg.SSHPath == "" {
		cfg.SSHPath = "ssh.exe"
	}
	if len(cfg.SSHArgs) == 0 {
		return 2, fmt.Errorf("missing ssh arguments")
	}

	if err := termutil.EnableVT(os.Stdout); err != nil {
		return 1, err
	}

	width, height := termutil.Size(os.Stdout)
	pty, err := conpty.New(width, height, 0)
	if err != nil {
		return 1, fmt.Errorf("create conpty: %w", err)
	}
	defer pty.Close()

	argv := append([]string{cfg.SSHPath}, cfg.SSHArgs...)
	pid, handle, err := pty.Spawn(cfg.SSHPath, argv, &syscall.ProcAttr{Env: os.Environ()})
	if err != nil {
		return 1, fmt.Errorf("spawn ssh: %w", err)
	}
	processHandle := windows.Handle(handle)
	defer windows.CloseHandle(processHandle)

	if cfg.Log != nil {
		_ = cfg.Log.Write(logging.EventSessionStart, map[string]any{
			"ssh_path": cfg.SSHPath,
			"ssh_args": cfg.SSHArgs,
			"pid":      pid,
			"width":    width,
			"height":   height,
		})
	}

	rawState, err := termutil.MakeRaw(os.Stdin)
	if err != nil {
		return 1, err
	}
	defer rawState.Restore()

	redactor := redact.New(cfg.Redact)
	done := make(chan struct{})
	errCh := make(chan error, 3)

	go bridgeOutput(pty, redactor, done, errCh)
	go bridgeInput(pty, cfg, redactor, done, errCh)
	go watchResize(pty, cfg.Log, done)

	waitCh := make(chan uint32, 1)
	go func() {
		_, _ = windows.WaitForSingleObject(processHandle, windows.INFINITE)
		var code uint32
		if err := windows.GetExitCodeProcess(processHandle, &code); err != nil {
			code = 1
		}
		waitCh <- code
	}()

	var exitCode uint32
	select {
	case exitCode = <-waitCh:
	case <-ctx.Done():
		_ = windows.TerminateProcess(processHandle, 1)
		exitCode = 1
	case err := <-errCh:
		if err != nil && !errors.Is(err, io.EOF) {
			_ = windows.TerminateProcess(processHandle, 1)
			return 1, err
		}
		exitCode = <-waitCh
	}

	close(done)
	if cfg.Log != nil {
		_ = cfg.Log.Write(logging.EventSessionEnd, map[string]any{
			"exit_code": exitCode,
		})
	}

	return int(exitCode), nil
}

func bridgeOutput(pty *conpty.ConPty, redactor *redact.Redactor, done <-chan struct{}, errCh chan<- error) {
	buf := make([]byte, 32*1024)
	for {
		select {
		case <-done:
			return
		default:
		}

		n, err := pty.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			redactor.ObserveOutput(chunk)
			_, _ = os.Stdout.Write(chunk)
		}
		if err != nil {
			errCh <- err
			return
		}
	}
}

func bridgeInput(pty *conpty.ConPty, cfg Config, redactor *redact.Redactor, done <-chan struct{}, errCh chan<- error) {
	buf := make([]byte, 8192)
	var acc []byte // accumulates consecutive key-down chunks until Enter

	flushAcc := func() {
		if cfg.Log == nil || len(acc) == 0 {
			acc = nil
			return
		}
		redacted := redactor.RedactInput(acc)
		data := input.EventData(acc, redacted, input.Options{RawInputLog: cfg.RawInputLog})

		// When the user recalls a command from shell history (ArrowUp /
		// ArrowDown + Enter), the command text comes from PTY output, not
		// from keyboard input.  Fall back to the current visible line.
		if text, _ := data["text"].(string); text == "" && hasHistoryNav(acc) {
			if line := redactor.CurrentLine(); line != "" {
				data["text"] = line
			}
		}

		_ = cfg.Log.Write(logging.EventInput, data)
		acc = nil
	}

	logChunk := func(chunk []byte) {
		if cfg.Log == nil {
			return
		}
		redacted := redactor.RedactInput(chunk)
		_ = cfg.Log.Write(logging.EventInput, input.EventData(chunk, redacted, input.Options{RawInputLog: cfg.RawInputLog}))
	}

	for {
		select {
		case <-done:
			return
		default:
		}

		n, err := os.Stdin.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)

			if _, writeErr := pty.Write(chunk); writeErr != nil {
				errCh <- writeErr
				return
			}

			// Release events are noise — skip logging entirely.
			if input.IsKeyRelease(chunk) {
				continue
			}

			mode := input.InputMode(chunk)
			if mode != "key" {
				// Focus / mouse / resize — flush any accumulated key stream first.
				flushAcc()
				logChunk(chunk)
				continue
			}

			// Accumulate key-down bytes.
			acc = append(acc, chunk...)

			// Enter ends a command line.
			if hasEnter(chunk) {
				flushAcc()
			}
		}
		if err != nil {
			errCh <- err
			return
		}
	}
}

func hasEnter(chunk []byte) bool {
	for _, k := range input.Keys(chunk) {
		if k == "Enter" {
			return true
		}
	}
	return false
}

func hasHistoryNav(chunk []byte) bool {
	for _, k := range input.Keys(chunk) {
		if k == "ArrowUp" || k == "ArrowDown" {
			return true
		}
	}
	return false
}

func watchResize(pty *conpty.ConPty, log *logging.Writer, done <-chan struct{}) {
	lastW, lastH := termutil.Size(os.Stdout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			w, h := termutil.Size(os.Stdout)
			if w == lastW && h == lastH {
				continue
			}
			lastW, lastH = w, h
			if err := pty.Resize(w, h); err == nil && log != nil {
				_ = log.Write(logging.EventResize, map[string]any{"width": w, "height": h})
			}
		}
	}
}
