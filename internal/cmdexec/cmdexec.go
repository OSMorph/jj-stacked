// Package cmdexec provides an abstraction for executing external commands.
package cmdexec

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CommandExecutor is the interface for executing external commands.
// AIDEV-NOTE: This abstraction enables dependency injection for testing.
type CommandExecutor interface {
	// Run executes a command and returns stdout.
	Run(ctx context.Context, name string, args ...string) (string, error)

	// RunWithStdin executes a command with stdin input.
	RunWithStdin(ctx context.Context, stdin string, name string, args ...string) (string, error)

	// RunInDir executes a command in a specific directory.
	RunInDir(ctx context.Context, dir string, name string, args ...string) (string, error)
}

// ExecError represents an error from command execution with exit code and stderr.
type ExecError struct {
	Command  string
	Args     []string
	ExitCode int
	Stderr   string
	Err      error
}

func (e *ExecError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("command %q failed (exit %d): %s", e.Command, e.ExitCode, strings.TrimSpace(e.Stderr))
	}
	return fmt.Sprintf("command %q failed (exit %d): %v", e.Command, e.ExitCode, e.Err)
}

func (e *ExecError) Unwrap() error {
	return e.Err
}

// RealExecutor executes commands using os/exec.
type RealExecutor struct {
	// WorkDir is an optional working directory override.
	WorkDir string
}

// NewRealExecutor creates a new RealExecutor.
func NewRealExecutor() *RealExecutor {
	return &RealExecutor{}
}

// NewRealExecutorInDir creates a RealExecutor with a specific working directory.
func NewRealExecutorInDir(dir string) *RealExecutor {
	return &RealExecutor{WorkDir: dir}
}

// Run executes a command and returns stdout.
func (r *RealExecutor) Run(ctx context.Context, name string, args ...string) (string, error) {
	return r.run(ctx, r.WorkDir, "", name, args...)
}

// RunWithStdin executes a command with stdin input.
func (r *RealExecutor) RunWithStdin(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	return r.run(ctx, r.WorkDir, stdin, name, args...)
}

// RunInDir executes a command in a specific directory.
func (r *RealExecutor) RunInDir(ctx context.Context, dir string, name string, args ...string) (string, error) {
	return r.run(ctx, dir, "", name, args...)
}

func (r *RealExecutor) run(ctx context.Context, dir, stdin, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	if dir != "" {
		cmd.Dir = dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	err := cmd.Run()
	if err != nil {
		exitCode := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return "", &ExecError{
			Command:  name,
			Args:     args,
			ExitCode: exitCode,
			Stderr:   stderr.String(),
			Err:      err,
		}
	}

	return stdout.String(), nil
}

// MockExecutor is a mock implementation for testing.
type MockExecutor struct {
	// Responses maps command signatures to expected stdout.
	// Key format: "name arg1 arg2 ..."
	Responses map[string]string

	// Errors maps command signatures to expected errors.
	Errors map[string]error

	// Calls records all commands that were executed.
	Calls []MockCall
}

// MockCall records a single command execution.
type MockCall struct {
	Name  string
	Args  []string
	Dir   string
	Stdin string
}

// NewMockExecutor creates a new MockExecutor.
func NewMockExecutor() *MockExecutor {
	return &MockExecutor{
		Responses: make(map[string]string),
		Errors:    make(map[string]error),
	}
}

// Run executes a mock command.
func (m *MockExecutor) Run(ctx context.Context, name string, args ...string) (string, error) {
	return m.run(ctx, "", "", name, args...)
}

// RunWithStdin executes a mock command with stdin.
func (m *MockExecutor) RunWithStdin(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	return m.run(ctx, "", stdin, name, args...)
}

// RunInDir executes a mock command in a directory.
func (m *MockExecutor) RunInDir(ctx context.Context, dir string, name string, args ...string) (string, error) {
	return m.run(ctx, dir, "", name, args...)
}

func (m *MockExecutor) run(ctx context.Context, dir, stdin, name string, args ...string) (string, error) {
	m.Calls = append(m.Calls, MockCall{
		Name:  name,
		Args:  args,
		Dir:   dir,
		Stdin: stdin,
	})

	key := m.makeKey(name, args...)

	if err, ok := m.Errors[key]; ok {
		return "", err
	}

	if resp, ok := m.Responses[key]; ok {
		return resp, nil
	}

	// If no exact match, try just the command name as fallback
	if err, ok := m.Errors[name]; ok {
		return "", err
	}

	if resp, ok := m.Responses[name]; ok {
		return resp, nil
	}

	return "", fmt.Errorf("mock: no response configured for %q", key)
}

func (m *MockExecutor) makeKey(name string, args ...string) string {
	parts := append([]string{name}, args...)
	return strings.Join(parts, " ")
}

// SetResponse configures a response for a command.
func (m *MockExecutor) SetResponse(response string, name string, args ...string) {
	key := m.makeKey(name, args...)
	m.Responses[key] = response
}

// SetError configures an error for a command.
func (m *MockExecutor) SetError(err error, name string, args ...string) {
	key := m.makeKey(name, args...)
	m.Errors[key] = err
}
