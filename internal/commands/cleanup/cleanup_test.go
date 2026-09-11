package cleanup

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	cleanupcore "github.com/OSMorph/jj-stacked/internal/cleanup"
	"github.com/OSMorph/jj-stacked/internal/testutil"
)

func TestPruneJSONNeverAppliesAndNoninteractiveNeedsConfirmation(t *testing.T) {
	r := testutil.NewRepository(t)
	r.Run(t, "describe", "-m", "Old draft")
	r.Write(t, "draft", "keep until selected\n")
	old := r.Change(t)
	r.Run(t, "new", "main")
	t.Chdir(r.Dir)
	cmd := NewPruneCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--stale", "--older-than", "1ns", "--json", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var plan cleanupcore.Plan
	if err := json.Unmarshal(output.Bytes(), &plan); err != nil {
		t.Fatalf("not JSON: %v %s", err, output.String())
	}
	if len(plan.Candidates) != 1 || plan.Candidates[0].Revision.CommitID != old.CommitID {
		t.Fatalf("plan=%+v", plan)
	}
	if !strings.Contains(plan.Candidates[0].DiffSummary, "draft") {
		t.Fatal("preview missing affected file summary")
	}
	if err := cleanupcore.CheckOperation(t.Context(), r.JJ, &plan); err != nil {
		t.Fatal("JSON mutated repository", err)
	}
	cmd = NewPruneCommand()
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--stale", "--older-than", "1ns"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("noninteractive pruning accepted without --yes")
	}
	if err := cleanupcore.CheckOperation(t.Context(), r.JJ, &plan); err != nil {
		t.Fatal("declined command mutated repository", err)
	}
}

func TestAbandonNeverChoosesKeeperFromYes(t *testing.T) {
	cmd := NewAbandonCommand()
	cmd.SetArgs([]string{"--diverged", "--yes"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--keep") {
		t.Fatalf("expected explicit keeper error, got %v", err)
	}
}

func TestPruneRequiresExplicitBatchScope(t *testing.T) {
	cmd := NewPruneCommand()
	cmd.SetArgs([]string{"--merged=false", "--yes"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "explicit") {
		t.Fatalf("expected explicit scope error, got %v", err)
	}
}
