package diagnostics

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// Fields represent structured key/value metadata for log entries.
type Fields map[string]any

// Logger writes structured JSON log lines.
type Logger struct {
	writer io.Writer
	now    func() time.Time
	mu     sync.Mutex
}

// NewLogger construct a structured logger.
//
// Log entries are encoded as single-line JSON objects.
//
// Returns a logger instance.
func NewLogger(writer io.Writer) *Logger {
	return &Logger{writer: writer, now: func() time.Time { return time.Now().UTC() }}
}

// Info write an info-level structured log event.
//
// Adds timestamp, level, message, and optional metadata fields.
//
// Returns nothing.
func (l *Logger) Info(message string, fields Fields) {
	l.log("info", message, fields)
}

// Warn write a warn-level structured log event.
//
// Adds timestamp, level, message, and optional metadata fields.
//
// Returns nothing.
func (l *Logger) Warn(message string, fields Fields) {
	l.log("warn", message, fields)
}

// Error write an error-level structured log event.
//
// Adds timestamp, level, message, and optional metadata fields.
//
// Returns nothing.
func (l *Logger) Error(message string, fields Fields) {
	l.log("error", message, fields)
}

func (l *Logger) log(level, message string, fields Fields) {
	entry := map[string]any{
		"timestamp": l.now().UTC().Format(time.RFC3339),
		"level":     level,
		"message":   message,
	}

	for key, value := range fields {
		entry[key] = value
	}

	encoded, err := json.Marshal(entry)
	if err != nil {
		fallbackEntry := map[string]any{
			"timestamp":      l.now().UTC().Format(time.RFC3339),
			"level":          level,
			"message":        message,
			"encoding_error": err.Error(),
		}

		encoded, err = json.Marshal(fallbackEntry)
		if err != nil {
			return
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.writer.Write(append(encoded, '\n'))
}
