package main

import (
	"context"
	"flag"
	"fmt"
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
	return filepath.Join("logs", name)
}
