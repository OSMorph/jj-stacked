package sync

import (
	"context"
	"strings"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

func TestConflictsErrorNamesConflictedRevisions(t *testing.T) {
	jj := &fakeJJ{getLog: func(_ context.Context, revset string, _ int) ([]jjutils.LogEntry, error) {
		if revset != "conflicts()" {
			return nil, nil
		}
		return []jjutils.LogEntry{{ChangeID: "z", CommitID: "abc123", Description: "stale conflict"}}, nil
	}}
	err := conflictsError(context.Background(), jj)
	if err == nil {
		t.Fatal("expected diagnostics")
	}
	for _, want := range []string{"conflicted revisions found in the repository", "stale conflict", "jj log -r 'conflicts()'"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("missing %q in %q", want, err.Error())
		}
	}
}

func TestConflictsErrorWithoutEntries(t *testing.T) {
	jj := &fakeJJ{getLog: func(_ context.Context, _ string, _ int) ([]jjutils.LogEntry, error) {
		return nil, nil
	}}
	err := conflictsError(context.Background(), jj)
	if err == nil || strings.Contains(err.Error(), "stale") {
		t.Fatalf("unexpected diagnostics: %v", err)
	}
}
