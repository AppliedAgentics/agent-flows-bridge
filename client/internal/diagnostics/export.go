package diagnostics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// BundleInput define diagnostics payload components to export.
type BundleInput struct {
	Config   map[string]any
	Health   map[string]any
	Logs     []string
	Metadata map[string]any
}

// ExporterOptions configure diagnostics export destination and clock.
type ExporterOptions struct {
	OutputDir string
	Now       func() time.Time
}

// Exporter writes diagnostics bundles to disk.
type Exporter struct {
	outputDir string
	now       func() time.Time
	counter   atomic.Uint64
}

// NewExporter construct diagnostics exporter.
//
// OutputDir defaults to the process temp directory when empty.
//
// Returns an exporter instance.
func NewExporter(opts ExporterOptions) *Exporter {
	outputDir := opts.OutputDir
	now := opts.Now

	if outputDir == "" {
		outputDir = os.TempDir()
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	return &Exporter{outputDir: outputDir, now: now}
}

// Export write a diagnostics bundle directory and return its path.
//
// Bundle includes config snapshot, health report, metadata, and JSONL logs.
// Metadata is augmented with `exported_at`.
//
// Returns absolute bundle path or an error.
func (e *Exporter) Export(input BundleInput) (string, error) {
	now := e.now().UTC()
	sequence := e.counter.Add(1)
	bundleName := fmt.Sprintf("diagnostics-%s-%03d", now.Format("20060102T150405Z"), sequence)
	bundlePath := filepath.Join(e.outputDir, bundleName)

	if err := os.MkdirAll(bundlePath, 0o755); err != nil {
		return "", fmt.Errorf("create bundle directory: %w", err)
	}

	if err := writeJSON(filepath.Join(bundlePath, "config.json"), normalizeMap(input.Config), 0o600); err != nil {
		return "", err
	}
	if err := writeJSON(filepath.Join(bundlePath, "health.json"), normalizeMap(input.Health), 0o600); err != nil {
		return "", err
	}

	metadata := normalizeMap(input.Metadata)
	metadata["exported_at"] = now.Format(time.RFC3339)
	if err := writeJSON(filepath.Join(bundlePath, "metadata.json"), metadata, 0o600); err != nil {
		return "", err
	}

	if err := writeLogs(filepath.Join(bundlePath, "logs.jsonl"), input.Logs); err != nil {
		return "", err
	}

	return bundlePath, nil
}

func writeJSON(path string, payload map[string]any, mode os.FileMode) error {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := writeFileAtomically(path, append(encoded, '\n'), mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeLogs(path string, logs []string) error {
	content := strings.Join(logs, "\n")
	if content != "" {
		content += "\n"
	}

	if err := writeFileAtomically(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}

func writeFileAtomically(path string, content []byte, mode os.FileMode) error {
	parentDir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(parentDir, "diagnostics-*.tmp")
	if err != nil {
		return err
	}

	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if err := tempFile.Chmod(mode); err != nil {
		_ = tempFile.Close()
		return err
	}
	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}

	return nil
}

func normalizeMap(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	return payload
}
