package gateway

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
)

const maxCommandOutputBytes = 64 * 1024

type shellCommandRunner struct{}

func (shellCommandRunner) Run(ctx context.Context, workdir string, command string) (CommandResult, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Dir = workdir
	var stdout cappedOutput
	var stderr cappedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := CommandResult{
		Status: "completed",
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err == nil {
		return result, nil
	}
	result.Status = "failed"
	if exitError, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitError.ExitCode()
		return result, nil
	}
	result.ExitCode = -1
	if result.Stderr == "" {
		result.Stderr = err.Error()
	}
	return result, nil
}

type codexAgentRunner struct{}

func (codexAgentRunner) RunCodex(ctx context.Context, workdir string, prompt string) (CommandResult, error) {
	cmd := exec.CommandContext(ctx,
		"codex",
		"exec",
		"--cd", workdir,
		"--sandbox", "workspace-write",
		"--skip-git-repo-check",
		"--json",
		prompt,
	)
	var stdout cappedOutput
	var stderr cappedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if apiKey, ok := ctx.Value("openai_api_key").(string); ok && apiKey != "" {
		cmd.Env = append(os.Environ(), "OPENAI_API_KEY="+apiKey)
	}

	err := cmd.Run()
	result := CommandResult{
		Status: "completed",
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err == nil {
		return result, nil
	}
	result.Status = "failed"
	if exitError, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitError.ExitCode()
		return result, nil
	}
	result.ExitCode = -1
	if result.Stderr == "" {
		result.Stderr = err.Error()
	}
	return result, nil
}

type cappedOutput struct {
	buffer    bytes.Buffer
	truncated bool
}

func (o *cappedOutput) Write(p []byte) (int, error) {
	remaining := maxCommandOutputBytes - o.buffer.Len()
	if remaining <= 0 {
		o.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = o.buffer.Write(p[:remaining])
		o.truncated = true
		return len(p), nil
	}
	_, _ = o.buffer.Write(p)
	return len(p), nil
}

func (o *cappedOutput) String() string {
	return o.buffer.String()
}

func (o *cappedOutput) Truncated() bool {
	return o.truncated
}
