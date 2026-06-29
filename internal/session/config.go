package session

import "github.com/logn-xu/WorkOfPoetry/internal/logging"

// Config controls an SSH audit session.
type Config struct {
	SSHPath     string
	SSHArgs     []string
	Log         *logging.Writer
	SessionID   string
	Redact      bool
	RawInputLog bool
}
