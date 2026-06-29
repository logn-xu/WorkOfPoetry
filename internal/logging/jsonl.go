package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	EventSessionStart = "session_start"
	EventInput        = "input"
	EventControl      = "control"
	EventResize       = "resize"
	EventSessionEnd   = "session_end"
)

// Event is a single JSONL audit event.
type Event struct {
	Type      string         `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	SessionID string         `json:"session_id"`
	Seq       uint64         `json:"seq,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// Writer writes audit events as newline-delimited JSON.
type Writer struct {
	mu        sync.Mutex
	file      *os.File
	encoder   *json.Encoder
	sessionID string
	seq       uint64
}

// New creates a JSONL writer at path. Parent directories are created as needed.
func New(path string, sessionID string) (*Writer, error) {
	if path == "" {
		return nil, fmt.Errorf("log path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	return &Writer{
		file:      file,
		encoder:   json.NewEncoder(file),
		sessionID: sessionID,
	}, nil
}

// Write records an event and assigns timestamp/session metadata.
func (w *Writer) Write(eventType string, data map[string]any) error {
	if w == nil {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.seq++
	event := Event{
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		SessionID: w.sessionID,
		Seq:       w.seq,
		Data:      data,
	}
	if err := w.encoder.Encode(event); err != nil {
		return fmt.Errorf("write log event: %w", err)
	}
	return nil
}

// Close closes the underlying log file.
func (w *Writer) Close() error {
	if w == nil || w.file == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}
