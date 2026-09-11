// Package common contains command setup and small text prompts.
package common

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/OSMorph/jj-stacked/internal/logger"
)

// Debug reads the shared flag, falling back to the environment.
func Debug(cmd *cobra.Command) bool {
	if flag := cmd.Flag("debug"); flag != nil && flag.Changed {
		return flag.Value.String() == "true"
	}
	return os.Getenv("JJ_STACK_DEBUG") != ""
}

// NewLogger applies the same environment defaults to every command.
func NewLogger(debug bool) *logger.Logger {
	return logger.New(logger.Options{Debug: debug, Format: os.Getenv("JJ_STACK_LOG_FORMAT")})
}

// Interactive reports whether input comes from a terminal.
func Interactive(input io.Reader) bool {
	file, ok := input.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}

// Prompt reads a single line without consuming subsequent answers.
type Prompt struct {
	input *bufio.Reader
	out   io.Writer
}

// NewPrompt creates one reader for a sequence of prompts.
func NewPrompt(input io.Reader, output io.Writer) *Prompt {
	return &Prompt{input: bufio.NewReader(input), out: output}
}

// Line asks for one answer. EOF and cancellation never imply approval.
func (p *Prompt) Line(question string) (string, error) {
	if _, err := fmt.Fprint(p.out, question); err != nil {
		return "", err
	}
	answer, err := p.input.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(answer), nil
}

// Confirm defaults to declining.
func (p *Prompt) Confirm(question string) (bool, error) {
	answer, err := p.Line(question + " [y/N] ")
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), err
}

// Select chooses numbered entries; an empty answer selects nothing.
func (p *Prompt) Select(count int, multiple bool) ([]int, error) {
	question := "Select a number (Enter to skip): "
	if multiple {
		question = "Select numbers separated by commas, or all (Enter to skip): "
	}
	answer, err := p.Line(question)
	if err != nil || answer == "" {
		return nil, err
	}
	if multiple && answer == "all" {
		indices := make([]int, count)
		for i := range indices {
			indices[i] = i
		}
		return indices, nil
	}
	var indices []int
	seen := map[int]bool{}
	for _, value := range strings.Split(answer, ",") {
		number, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || number < 1 || number > count || !multiple && len(indices) > 0 {
			return nil, fmt.Errorf("invalid selection; choose a listed number between 1 and %d", count)
		}
		if !seen[number] {
			indices = append(indices, number-1)
			seen[number] = true
		}
	}
	return indices, nil
}
