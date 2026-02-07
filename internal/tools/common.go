package tools

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ToolSpec defines how to invoke an external tool.
type ToolSpec struct {
	Name       string
	BinaryName string
	Args       []string
	Timeout    time.Duration
}

// ToolResult captures the outcome of a tool execution.
type ToolResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
	Error    error
}

// OutputLine represents a single line of real-time output.
type OutputLine struct {
	Timestamp time.Time `json:"timestamp"`
	Stream    string    `json:"stream"`
	Line      string    `json:"line"`
	Done      bool      `json:"done,omitempty"`
}

// CheckInstalled verifies that a tool binary exists on PATH.
func CheckInstalled(binaryName string) (string, error) {
	path, err := exec.LookPath(binaryName)
	if err != nil {
		return "", fmt.Errorf("%s is not installed or not on PATH", binaryName)
	}
	return path, nil
}

// Run executes a tool and sends each line of output to the channel.
// The channel is closed when the tool exits.
func Run(ctx context.Context, spec ToolSpec, output chan<- OutputLine) *ToolResult {
	defer close(output)

	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, spec.Timeout)
		defer cancel()
	}

	start := time.Now()

	cmd := exec.CommandContext(ctx, spec.BinaryName, spec.Args...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return &ToolResult{ExitCode: -1, Error: fmt.Errorf("stdout pipe: %w", err), Duration: time.Since(start)}
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return &ToolResult{ExitCode: -1, Error: fmt.Errorf("stderr pipe: %w", err), Duration: time.Since(start)}
	}

	if err := cmd.Start(); err != nil {
		return &ToolResult{ExitCode: -1, Error: fmt.Errorf("start: %w", err), Duration: time.Since(start)}
	}

	var stdoutBuf, stderrBuf strings.Builder

	done := make(chan struct{})

	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			stdoutBuf.WriteString(line)
			stdoutBuf.WriteByte('\n')
			output <- OutputLine{Timestamp: time.Now(), Stream: "stdout", Line: line}
		}
		done <- struct{}{}
	}()

	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			stderrBuf.WriteString(line)
			stderrBuf.WriteByte('\n')
			output <- OutputLine{Timestamp: time.Now(), Stream: "stderr", Line: line}
		}
		done <- struct{}{}
	}()

	// Wait for both readers to finish
	<-done
	<-done

	exitCode := 0
	waitErr := cmd.Wait()
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return &ToolResult{
		ExitCode: exitCode,
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		Duration: time.Since(start),
		Error:    waitErr,
	}
}
