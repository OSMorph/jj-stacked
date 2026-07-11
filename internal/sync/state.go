package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

// AIDEV-NOTE: State persistence enables --continue and --abort flags.
// The state file is stored in .jj/jj-stacked-sync.json to keep it
// with the jj repository metadata.

const stateFileName = "jj-stacked-sync.json"

// getStateFilePath returns the path to the state file.
// It is stored in the .jj directory of the repository root.
func getStateFilePath(ctx context.Context, jj jjutils.JJFunctions) (string, error) {
	root, err := jj.GetRepoRoot(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get repo root: %w", err)
	}

	return filepath.Join(root, ".jj", stateFileName), nil
}

// SaveSyncState persists the sync state to disk.
func SaveSyncState(ctx context.Context, jj jjutils.JJFunctions, state *SyncState) error {
	path, err := getStateFilePath(ctx, jj)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	temp, err := os.CreateTemp(filepath.Dir(path), ".jj-stacked-sync-*")
	if err != nil {
		return fmt.Errorf("failed to create state file: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	err = temp.Chmod(0o600)
	if err == nil {
		_, err = temp.Write(data)
	}
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tempPath, path)
	}
	if err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// LoadSyncState reads the persisted sync state from disk.
// Returns nil if no state file exists.
func LoadSyncState(ctx context.Context, jj jjutils.JJFunctions) (*SyncState, error) {
	path, err := getStateFilePath(ctx, jj)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state SyncState
	if err := json.Unmarshal(data, &state); err != nil {
		// Corrupted state file - return error but allow clearing it
		return nil, fmt.Errorf("corrupted state file (delete %s to clear): %w", path, err)
	}

	return &state, nil
}

// ClearSyncState removes the persisted state file.
func ClearSyncState(ctx context.Context, jj jjutils.JJFunctions) error {
	path, err := getStateFilePath(ctx, jj)
	if err != nil {
		return err
	}

	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove state file: %w", err)
	}

	return nil
}

// HasPendingSync checks if a sync operation is in progress.
func HasPendingSync(ctx context.Context, jj jjutils.JJFunctions) (bool, error) {
	state, err := LoadSyncState(ctx, jj)
	if err != nil {
		return false, err
	}

	return state != nil, nil
}

// CreateInitialState creates a new sync state for a starting sync operation.
func CreateInitialState(plan *SyncPlan, operationID, bookmark string, noResubmit bool) *SyncState {
	var pendingSteps []string
	for _, bm := range plan.ToDelete {
		pendingSteps = append(pendingSteps, fmt.Sprintf("delete:%s", bm))
	}

	// Add abandon steps
	for _, bm := range plan.ToAbandon {
		pendingSteps = append(pendingSteps, fmt.Sprintf("abandon:%s", bm))
	}

	// Add rebase step if needed
	for _, root := range plan.RebaseRoots {
		pendingSteps = append(pendingSteps, fmt.Sprintf("rebase:%s", root))
	}
	for _, bm := range plan.ToPush {
		pendingSteps = append(pendingSteps, fmt.Sprintf("push:%s", bm))
	}
	if !noResubmit {
		pendingSteps = append(pendingSteps, "refresh-prs")
	}

	return &SyncState{
		StartedAt:        time.Now(),
		Plan:             plan,
		PendingSteps:     pendingSteps,
		Phase:            "initialized",
		StartOperationID: operationID,
		Bookmark:         bookmark,
		Remote:           plan.Remote,
		NoResubmit:       noResubmit,
	}
}

// MarkStepComplete moves a step from pending to completed.
func (s *SyncState) MarkStepComplete(step string) {
	if s.StepComplete(step) {
		return
	}
	// Remove from pending
	for i, pending := range s.PendingSteps {
		if pending == step {
			s.PendingSteps = append(s.PendingSteps[:i], s.PendingSteps[i+1:]...)
			break
		}
	}

	// Add to completed
	s.CompletedSteps = append(s.CompletedSteps, step)
}

// StepComplete reports whether a durable execution step is complete.
func (s *SyncState) StepComplete(step string) bool {
	for _, completed := range s.CompletedSteps {
		if completed == step {
			return true
		}
	}
	return false
}

// SetPhase updates the current phase.
func (s *SyncState) SetPhase(phase string) {
	s.Phase = phase
}

// SetConflictFiles records the files with conflicts.
func (s *SyncState) SetConflictFiles(files []string) {
	s.ConflictFiles = files
}

// IsComplete returns true if all steps are completed.
func (s *SyncState) IsComplete() bool {
	return len(s.PendingSteps) == 0
}
