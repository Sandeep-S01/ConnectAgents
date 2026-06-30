package logging_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"personal-ai-assistant/internal/logging"
)

func TestRotatingFileWriterRotatesWhenMaxBytesExceeded(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "gateway.log")
	writer, err := logging.NewRotatingFileWriter(logPath, 12)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	defer writer.Close()

	if _, err := writer.Write([]byte("first-line\n")); err != nil {
		t.Fatalf("write first line: %v", err)
	}
	if _, err := writer.Write([]byte("second-line\n")); err != nil {
		t.Fatalf("write second line: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	active, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read active log: %v", err)
	}
	if !strings.Contains(string(active), "second-line") {
		t.Fatalf("expected active log to contain second line, got %q", active)
	}

	rotated, err := os.ReadFile(logPath + ".1")
	if err != nil {
		t.Fatalf("read rotated log: %v", err)
	}
	if !strings.Contains(string(rotated), "first-line") {
		t.Fatalf("expected rotated log to contain first line, got %q", rotated)
	}
}
