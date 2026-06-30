package gateway

import (
	"context"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestShellCommandRunnerCapsStdout(t *testing.T) {
	t.Parallel()

	command := largeStdoutCommand()
	result, err := shellCommandRunner{}.Run(context.Background(), t.TempDir(), command)
	if err != nil {
		t.Fatalf("run command: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("expected completed status, got %q: %s", result.Status, result.Stderr)
	}
	if len(result.Stdout) != maxCommandOutputBytes {
		t.Fatalf("expected stdout capped to %d bytes, got %d", maxCommandOutputBytes, len(result.Stdout))
	}
}

func TestCappedOutputWriterReportsTruncation(t *testing.T) {
	t.Parallel()

	var output cappedOutput
	written, err := output.Write([]byte(strings.Repeat("x", maxCommandOutputBytes+1)))
	if err != nil {
		t.Fatalf("write output: %v", err)
	}
	if written != maxCommandOutputBytes+1 {
		t.Fatalf("expected full write count, got %d", written)
	}
	if !output.Truncated() {
		t.Fatal("expected output to report truncation")
	}
	if len(output.String()) != maxCommandOutputBytes {
		t.Fatalf("expected capped output length %d, got %d", maxCommandOutputBytes, len(output.String()))
	}
}

func largeStdoutCommand() string {
	if runtime.GOOS == "windows" {
		return "$OutputEncoding = [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new(); [Console]::Out.Write(('a' * " + outputLimitPlusOne() + "))"
	}
	return "head -c " + outputLimitPlusOne() + " /dev/zero | tr '\\0' a"
}

func outputLimitPlusOne() string {
	return strconv.Itoa(maxCommandOutputBytes + 1)
}
