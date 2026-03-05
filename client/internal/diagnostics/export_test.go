package diagnostics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExportBundleWritesExpectedFiles(t *testing.T) {
	exporter := NewExporter(ExporterOptions{OutputDir: t.TempDir()})

	input := BundleInput{
		Config: map[string]any{
			"api_base_url": "https://saas.example.test",
			"runtime_url":  "http://127.0.0.1:18789",
		},
		Health: map[string]any{
			"gateway_reachable": true,
		},
		Logs: []string{
			`{"level":"info","message":"hello"}`,
			`{"level":"warn","message":"retrying"}`,
		},
		Metadata: map[string]any{
			"app_version": "0.1.0",
		},
	}

	bundlePath, err := exporter.Export(input)
	if err != nil {
		t.Fatalf("export bundle: %v", err)
	}

	requiredFiles := []string{"config.json", "health.json", "logs.jsonl", "metadata.json"}
	for _, fileName := range requiredFiles {
		fullPath := filepath.Join(bundlePath, fileName)
		if _, err := os.Stat(fullPath); err != nil {
			t.Fatalf("expected file %s: %v", fullPath, err)
		}
	}

	metadataRaw, err := os.ReadFile(filepath.Join(bundlePath, "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}

	var metadata map[string]any
	if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}

	if metadata["app_version"] != "0.1.0" {
		t.Fatalf("unexpected app_version: %v", metadata["app_version"])
	}
	if metadata["exported_at"] == nil {
		t.Fatal("expected exported_at metadata")
	}
}

func TestExportBundleCreatesUniquePathsPerRun(t *testing.T) {
	exporter := NewExporter(ExporterOptions{OutputDir: t.TempDir(), Now: fixedNow})

	input := BundleInput{}

	firstPath, err := exporter.Export(input)
	if err != nil {
		t.Fatalf("first export: %v", err)
	}

	secondPath, err := exporter.Export(input)
	if err != nil {
		t.Fatalf("second export: %v", err)
	}

	if firstPath == secondPath {
		t.Fatalf("expected unique bundle paths, got %s", firstPath)
	}
}

func TestWriteJSONLeavesTargetAbsentWhenEncodingFails(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "metadata.json")

	err := writeJSON(targetPath, map[string]any{"bad": make(chan int)}, 0o600)
	if err == nil {
		t.Fatal("expected encoding error")
	}

	if _, statErr := os.Stat(targetPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected target file to remain absent, got %v", statErr)
	}
}

func TestWriteLogsWritesJSONLinesWithTrailingNewlines(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "logs.jsonl")

	err := writeLogs(targetPath, []string{
		`{"level":"info","message":"hello"}`,
		`{"level":"warn","message":"retrying"}`,
	})
	if err != nil {
		t.Fatalf("write logs: %v", err)
	}

	raw, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}

	expected := "{\"level\":\"info\",\"message\":\"hello\"}\n{\"level\":\"warn\",\"message\":\"retrying\"}\n"
	if string(raw) != expected {
		t.Fatalf("unexpected logs content: got=%q want=%q", string(raw), expected)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 3, 4, 13, 0, 0, 0, time.UTC)
}
