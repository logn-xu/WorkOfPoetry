package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/logn-xu/WorkOfPoetry/internal/logging"
	"github.com/logn-xu/WorkOfPoetry/internal/session"
)

func main() {
	exitCode, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "workofpoetry:", err)
	}
	os.Exit(exitCode)
}

func run() (int, error) {
	var sshPath string
	var logPath string
	var sessionID string
	var redact bool
	var rawInputLog bool

	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flags.StringVar(&sshPath, "ssh-path", "ssh.exe", "path to ssh.exe")
	flags.StringVar(&logPath, "log", defaultLogPath(), "JSONL audit log path")
	flags.StringVar(&sessionID, "session-id", defaultSessionID(), "session identifier")
	flags.BoolVar(&redact, "redact", true, "redact input after password/passphrase-like prompts")
	flags.BoolVar(&rawInputLog, "raw-input-log", false, "include raw input chunks as base64 in logs")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: %s [flags] -- <ssh args...>\n", filepath.Base(os.Args[0]))
		flags.PrintDefaults()
	}
	if err := flags.Parse(os.Args[1:]); err != nil {
		return 2, err
	}

	sshArgs := flags.Args()
	if len(sshArgs) > 0 && sshArgs[0] == "--" {
		sshArgs = sshArgs[1:]
	}
	if len(sshArgs) == 0 {
		flags.Usage()
		return 2, fmt.Errorf("missing ssh arguments")
	}

	writer, err := logging.New(logPath, sessionID)
	if err != nil {
		return 1, err
	}
	defer writer.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return session.Run(ctx, session.Config{
		SSHPath:     sshPath,
		SSHArgs:     sshArgs,
		Log:         writer,
		SessionID:   sessionID,
		Redact:      redact,
		RawInputLog: rawInputLog,
	})
}

func defaultSessionID() string {
	return time.Now().UTC().Format("20060102T150405.000000000Z")
}

func defaultLogPath() string {
	id := defaultSessionID()
	name := "ssh-session-" + strings.NewReplacer(":", "", ".", "-").Replace(id) + ".jsonl"

	dir, err := defaultLogDir()
	if err != nil {
		// Last-resort: a per-user cache directory.
		dir = fallbackLogDir()
	}
	return filepath.Join(dir, name)
}

// defaultLogDir returns a writable log directory that lives next to the
// workofpoetry executable, so the audit trail is always co-located with
// the binary the operator launched.  When that location is not writable
// (for example when the binary lives in "C:\Program Files\..." and was
// installed there) we transparently fall back to a per-user cache
// directory so the program still starts.
func defaultLogDir() (string, error) {
	exeDir, err := exeDirectory()
	if err != nil {
		return "", err
	}
	return defaultLogDirFromExe(exeDir)
}

// defaultLogDirFromExe resolves a "<exeDir>/logs" directory and verifies
// it is writable.  Split out from defaultLogDir so tests can drive it
// with a fake executable path.
func defaultLogDirFromExe(exeDir string) (string, error) {
	dir := filepath.Join(exeDir, "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		if !isAccessDenied(err) && !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		if errors.Is(err, fs.ErrExist) {
			// Already exists — only acceptable if we can write to it.
			if writable(dir) {
				return dir, nil
			}
		}
		return "", errAccessDenied
	}
	if !writable(dir) {
		return "", errAccessDenied
	}
	return dir, nil
}

// exeDirectory returns the directory that holds the running executable,
// with symlinks and any "go run" build-time indirection resolved.
//
// On Windows, `os.Executable` can return a path like
// "\\?\C:\...\workofpoetry.exe" (extended-length form) when the binary
// is launched from a UNC location, or "<temp>\go-build...\exe\workofpoetry.exe"
// when invoked through `go run`.  EvalSymlinks + Abs + Clean normalise
// both cases.
func exeDirectory() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return exeDirectoryFromPath(exe)
}

// exeDirectoryFromPath is the path-driven equivalent of exeDirectory,
// used by tests to avoid calling os.Executable (which would return the
// test binary, not the fake one).
func exeDirectoryFromPath(exe string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		exe = resolved
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Clean(abs)), nil
}

// writable reports whether the given directory accepts new files.  It
// uses a probe file with O_EXCL so a stale probe from a previous run
// cannot fool us.
func writable(dir string) bool {
	probe := filepath.Join(dir, ".workofpoetry-write-probe")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return true
}

func isAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrPermission) {
		return true
	}
	// Windows often returns a wrapped Win32 error code 5 (ERROR_ACCESS_DENIED)
	// or 32 (ERROR_SHARING_VIOLATION); match the message heuristically.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "access is denied") ||
		strings.Contains(msg, "permission denied")
}

var errAccessDenied = errors.New("default log directory is not writable")

// fallbackLogDir returns a per-user directory under the OS-conventional
// cache location.  Used only when the directory next to the executable
// is not writable (e.g. read-only installation in "C:\Program Files").
func fallbackLogDir() string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		// os.TempDir is the final safety net; it is always writable.
		base = os.TempDir()
	}
	dir := filepath.Join(base, "workofpoetry", "logs")
	// Best-effort: if even this fails the caller's MkdirAll will surface
	// the error; we don't return it because the user hasn't asked for a
	// specific log path.
	_ = os.MkdirAll(dir, 0o700)
	return dir
}
