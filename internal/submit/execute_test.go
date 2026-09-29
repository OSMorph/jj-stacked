package submit

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

type recordingGitHub struct {
	*fakeGitHubClient
	events       []string
	bodies       map[int]string
	createErr    error
	updateErr    error
	updateErrors map[int]error
}

func (g *recordingGitHub) CreatePullRequest(_ context.Context, _, _ string, req *github.CreatePRRequest) (*github.PullRequest, error) {
	g.events = append(g.events, "create:"+req.Head)
	if g.createErr != nil {
		return nil, g.createErr
	}
	pr := &github.PullRequest{Number: len(g.prsByHead) + 1, Head: req.Head, Base: req.Base, URL: "https://example.test/1"}
	g.prsByHead[req.Head] = pr
	return pr, nil
}
func (g *recordingGitHub) UpdatePullRequest(_ context.Context, _, _ string, number int, req *github.UpdatePRRequest) (*github.PullRequest, error) {
	base := ""
	if req.Base != nil {
		base = *req.Base
	}
	g.events = append(g.events, fmt.Sprintf("update:%d:%s", number, base))
	if g.updateErr != nil {
		return nil, g.updateErr
	}
	if err := g.updateErrors[number]; err != nil {
		return nil, err
	}
	for _, pr := range g.prsByHead {
		if pr.Number == number && req.Base != nil {
			pr.Base = *req.Base
		}
	}
	return nil, nil
}
func (g *recordingGitHub) CreateComment(_ context.Context, _, _ string, number int, body string) (*github.Comment, error) {
	g.events = append(g.events, "comment:create")
	g.bodies[number] = body
	comment := &github.Comment{ID: int64(number), Body: body}
	g.comments[number] = []*github.Comment{comment}
	return comment, nil
}
func (g *recordingGitHub) UpdateComment(_ context.Context, _, _ string, id int64, body string) (*github.Comment, error) {
	g.events = append(g.events, "comment:update")
	g.bodies[int(id)] = body
	comment := &github.Comment{ID: id, Body: body}
	g.comments[int(id)] = []*github.Comment{comment}
	return comment, nil
}
func newRecordingGitHub() *recordingGitHub {
	return &recordingGitHub{fakeGitHubClient: &fakeGitHubClient{prsByHead: map[string]*github.PullRequest{}, comments: map[int][]*github.Comment{}}, bodies: map[int]string{}}
}

type recordingJJ struct {
	jjutils.JJFunctions
	events     *[]string
	err        error
	pushErrors []error
}

func (j *recordingJJ) Push(context.Context, string, string) error {
	*j.events = append(*j.events, "push")
	if len(j.pushErrors) > 0 {
		err := j.pushErrors[0]
		j.pushErrors = j.pushErrors[1:]
		return err
	}
	return j.err
}

func TestExecuteSubmissionResolvesCreatedPRWithoutMutatingPlan(t *testing.T) {
	gh := newRecordingGitHub()
	comment := &SyncCommentAction{Bookmark: "a", BaseBranch: "main", StackEntries: []github.StackEntry{{Bookmark: "a"}}}
	plan := &SubmissionPlan{Actions: []SubmissionAction{&PushAction{Bookmark: "a"}, &CreatePRAction{Bookmark: "a", BaseBranch: "main"}, comment}}
	result, err := ExecuteSubmissionPlan(context.Background(), plan, &ActionDeps{GitHub: gh, JJ: &recordingJJ{events: &gh.events}}, nil)
	if err != nil || result.Summary.Failed != 0 {
		t.Fatalf("result %+v: %v", result, err)
	}
	if !reflect.DeepEqual(gh.events, []string{"push", "create:a", "comment:create"}) {
		t.Fatalf("wrong order: %v", gh.events)
	}
	if comment.PRNumber != 0 || comment.StackEntries[0].PRNumber != 0 {
		t.Fatal("execution modified reviewed plan")
	}
	data, err := github.ParseStackComment(gh.bodies[1])
	if err != nil {
		t.Fatal(err)
	}
	if data.PRNumbers["a"] != 1 {
		t.Fatalf("created PR not carried into comment: %+v", data)
	}
	if result.Executed[1].CreatedPR == nil || len(GetCreatedPRURLs(result)) != 1 {
		t.Fatalf("missing typed result: %+v", result)
	}
}

func TestExecuteSubmissionStopsCriticalFailures(t *testing.T) {
	for _, fail := range []string{"protect", "push", "create", "update"} {
		t.Run(fail, func(t *testing.T) {
			gh := newRecordingGitHub()
			jj := &recordingJJ{events: &gh.events}
			failure := errors.New("fixture failure")
			switch fail {
			case "protect":
				gh.updateErr = failure
			case "push":
				jj.err = failure
			case "create":
				gh.createErr = failure
			case "update":
				gh.updateErr = failure
			}
			actions := []SubmissionAction{&PushAction{Bookmark: "a"}, &CreatePRAction{Bookmark: "a"}, &UpdateBaseAction{PRNumber: 1, NewBase: "main"}, &SyncCommentAction{Bookmark: "a", PRNumber: 1, BaseBranch: "main"}}
			if fail == "protect" {
				actions = append([]SubmissionAction{&UpdateBaseAction{PRNumber: 2, OldBase: "a", NewBase: "main", Protect: true}}, actions...)
			}
			plan := &SubmissionPlan{Actions: actions}
			result, err := ExecuteSubmissionPlan(context.Background(), plan, &ActionDeps{GitHub: gh, JJ: jj}, nil)
			if result.Summary.Failed != 1 {
				t.Fatalf("wrong failure count: %+v", result)
			}
			if !errors.Is(err, failure) || result.Summary.Skipped == 0 {
				t.Fatalf("critical failure continued: %+v %v", result, err)
			}
			for _, event := range gh.events {
				if event == "comment:create" {
					t.Fatal("comment followed critical failure")
				}
			}
		})
	}
}

func TestProtectFailureStopsEveryPush(t *testing.T) {
	gh := newRecordingGitHub()
	gh.updateErr = errors.New("cannot retarget")
	jj := &recordingJJ{events: &gh.events}
	plan := &SubmissionPlan{Actions: []SubmissionAction{
		&UpdateBaseAction{PRNumber: 2, OldBase: "a", NewBase: "main", Protect: true},
		&PushAction{Bookmark: "a"},
		&PushAction{Bookmark: "b"},
	}}
	result, err := ExecuteSubmissionPlan(context.Background(), plan, &ActionDeps{GitHub: gh, JJ: jj}, nil)
	if err == nil || !strings.Contains(err.Error(), "before any push") {
		t.Fatalf("error = %v", err)
	}
	if !reflect.DeepEqual(gh.events, []string{"update:2:main"}) || result.Summary.Skipped != 2 {
		t.Fatalf("events=%v result=%+v", gh.events, result)
	}
}

func TestSubmissionRerunsAfterProtectedPushFailure(t *testing.T) {
	gh := newRecordingGitHub()
	gh.prsByHead["a"] = &github.PullRequest{Number: 1, Head: "a", Base: "main"}
	gh.prsByHead["b"] = &github.PullRequest{Number: 2, Head: "b", Base: "a"}
	analysis := &AnalysisResult{Stack: []StackBookmark{
		{Bookmark: jjutils.Bookmark{Name: "b"}, NeedsPush: true},
		{Bookmark: jjutils.Bookmark{Name: "a"}, NeedsPush: true},
	}}
	deps := &PlanningDeps{GitHub: gh, Remote: "origin", DefaultBranch: "main"}
	plan, err := CreateSubmissionPlan(context.Background(), analysis, deps, nil)
	if err != nil {
		t.Fatal(err)
	}
	pushFailure := errors.New("push failed")
	jj := &recordingJJ{events: &gh.events, pushErrors: []error{nil, pushFailure}}
	result, err := ExecuteSubmissionPlan(context.Background(), plan, &ActionDeps{GitHub: gh, JJ: jj}, nil)
	if !errors.Is(err, pushFailure) || result.Summary.Failed != 1 || gh.prsByHead["b"].Base != "main" {
		t.Fatalf("first run result=%+v err=%v PR=%+v", result, err, gh.prsByHead["b"])
	}

	// A fresh analysis sees b at the remote and a still pending.
	analysis.Stack[0].NeedsPush = false
	rerun, err := CreateSubmissionPlan(context.Background(), analysis, deps, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range rerun.Actions {
		if action.Type() == ActionProtectBase {
			t.Fatalf("rerun re-protected already safe PR: %v", actionSignatures(rerun.Actions))
		}
	}
	if got := actionSignatures(rerun.Actions); len(got) == 0 || got[0] != "push:a" {
		t.Fatalf("rerun did not resume from remote state: %v", got)
	}
	gh.events = nil
	result, err = ExecuteSubmissionPlan(context.Background(), rerun, &ActionDeps{GitHub: gh, JJ: &recordingJJ{events: &gh.events}}, nil)
	if err != nil || result.Summary.Failed != 0 || gh.prsByHead["a"].Base != "b" || gh.prsByHead["b"].Base != "main" {
		t.Fatalf("rerun result=%+v err=%v PRs=%+v", result, err, gh.prsByHead)
	}
}

func TestSubmissionRerunsAfterPartialProtection(t *testing.T) {
	gh := newRecordingGitHub()
	gh.updateErrors = map[int]error{4: errors.New("cannot retarget d")}
	gh.prsByHead = map[string]*github.PullRequest{
		"a": {Number: 1, Head: "a", Base: "main"},
		"b": {Number: 2, Head: "b", Base: "a"},
		"c": {Number: 3, Head: "c", Base: "b"},
		"d": {Number: 4, Head: "d", Base: "c"},
	}
	analysis := &AnalysisResult{Stack: []StackBookmark{
		{Bookmark: jjutils.Bookmark{Name: "b"}, NeedsPush: true},
		{Bookmark: jjutils.Bookmark{Name: "c"}, NeedsPush: true},
		{Bookmark: jjutils.Bookmark{Name: "a"}, NeedsPush: true},
		{Bookmark: jjutils.Bookmark{Name: "d"}, NeedsPush: true},
	}}
	deps := &PlanningDeps{GitHub: gh, Remote: "origin", DefaultBranch: "main"}
	plan, err := CreateSubmissionPlan(context.Background(), analysis, deps, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteSubmissionPlan(context.Background(), plan, &ActionDeps{GitHub: gh, JJ: &recordingJJ{events: &gh.events}}, nil)
	if err == nil || result.Summary.Failed != 1 || gh.prsByHead["b"].Base != "main" {
		t.Fatalf("partial protection result=%+v err=%v PRs=%+v", result, err, gh.prsByHead)
	}
	delete(gh.updateErrors, 4)
	rerun, err := CreateSubmissionPlan(context.Background(), analysis, deps, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := actionSignatures(rerun.Actions)
	if len(got) == 0 || got[0] != "protect_base:4:c:main" {
		t.Fatalf("rerun actions=%v", got)
	}
}

func TestSubmissionRerunsAfterPartialFinalBaseUpdates(t *testing.T) {
	gh := newRecordingGitHub()
	gh.updateErrors = map[int]error{4: errors.New("cannot set final base")}
	gh.prsByHead = map[string]*github.PullRequest{
		"b": {Number: 2, Head: "b", Base: "main"},
		"a": {Number: 1, Head: "a", Base: "main"},
		"d": {Number: 4, Head: "d", Base: "main"},
	}
	plan := &SubmissionPlan{Actions: []SubmissionAction{
		&UpdateBaseAction{Bookmark: "a", PRNumber: 1, OldBase: "main", NewBase: "b"},
		&UpdateBaseAction{Bookmark: "d", PRNumber: 4, OldBase: "main", NewBase: "a"},
		&SyncCommentAction{Bookmark: "b", PRNumber: 2, BaseBranch: "main"},
		&SyncCommentAction{Bookmark: "a", PRNumber: 1, BaseBranch: "main"},
		&SyncCommentAction{Bookmark: "d", PRNumber: 4, BaseBranch: "main"},
	}}
	result, err := ExecuteSubmissionPlan(context.Background(), plan, &ActionDeps{GitHub: gh}, nil)
	if err == nil || result.Summary.Failed != 1 || gh.prsByHead["a"].Base != "b" || gh.prsByHead["d"].Base != "main" {
		t.Fatalf("partial final update result=%+v err=%v PRs=%+v", result, err, gh.prsByHead)
	}
	for _, event := range gh.events {
		if strings.HasPrefix(event, "comment:") {
			t.Fatalf("comment followed final base failure: %v", gh.events)
		}
	}

	delete(gh.updateErrors, 4)
	analysis := &AnalysisResult{Stack: []StackBookmark{
		{Bookmark: jjutils.Bookmark{Name: "b"}},
		{Bookmark: jjutils.Bookmark{Name: "a"}},
		{Bookmark: jjutils.Bookmark{Name: "d"}},
	}}
	rerun, err := CreateSubmissionPlan(context.Background(), analysis, &PlanningDeps{GitHub: gh, DefaultBranch: "main"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := actionSignatures(rerun.Actions)
	if len(got) == 0 || got[0] != "update_base:4:main:a" {
		t.Fatalf("rerun actions=%v", got)
	}
	gh.events = nil
	result, err = ExecuteSubmissionPlan(context.Background(), rerun, &ActionDeps{GitHub: gh}, nil)
	if err != nil || result.Summary.Failed != 0 || gh.prsByHead["d"].Base != "a" {
		t.Fatalf("rerun result=%+v err=%v events=%v", result, err, gh.events)
	}
}

func TestRefreshBaseFailureDoesNotWriteComments(t *testing.T) {
	gh := newRecordingGitHub()
	gh.updateErr = errors.New("cannot update")
	gh.prsByHead["a"] = &github.PullRequest{Number: 1, Head: "a", Base: "old"}
	analysis := &AnalysisResult{Stack: []StackBookmark{{Bookmark: jjutils.Bookmark{Name: "a"}, NeedsPush: true}}}
	plan, err := CreatePRRefreshPlan(context.Background(), analysis, &PlanningDeps{GitHub: gh, DefaultBranch: "main"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range plan.Actions {
		if action.Type() == ActionPush || action.Type() == ActionCreatePR || action.Type() == ActionProtectBase {
			t.Fatalf("refresh plan contains %s", action.Type())
		}
	}
	result, err := ExecuteSubmissionPlan(context.Background(), plan, &ActionDeps{GitHub: gh}, nil)
	if err == nil || result.Summary.Failed != 1 || !reflect.DeepEqual(gh.events, []string{"update:1:main"}) {
		t.Fatalf("events=%v result=%+v err=%v", gh.events, result, err)
	}
	if strings.Contains(err.Error(), "submit") || !strings.Contains(err.Error(), "sync --continue") {
		t.Fatalf("refresh recovery guidance = %q", err)
	}
}

func TestUnchangedSubmissionMakesNoGitHubWrites(t *testing.T) {
	gh := newRecordingGitHub()
	gh.prsByHead["a"] = &github.PullRequest{Number: 1, Head: "a", Base: "main", URL: "https://example.test/1"}
	analysis := &AnalysisResult{Stack: []StackBookmark{{Bookmark: jjutils.Bookmark{Name: "a"}}}}
	deps := &PlanningDeps{GitHub: gh, DefaultBranch: "main"}
	for run := 0; run < 3; run++ {
		plan, err := CreateSubmissionPlan(context.Background(), analysis, deps, nil)
		if err != nil {
			t.Fatal(err)
		}
		gh.events = nil
		result, err := ExecuteSubmissionPlan(context.Background(), plan, &ActionDeps{GitHub: gh}, nil)
		if err != nil || result.Summary.Failed > 0 {
			t.Fatalf("execute: %+v %v", result, err)
		}
		if run > 0 && len(gh.events) > 0 {
			t.Fatalf("unchanged submission wrote: %v", gh.events)
		}
	}
}

func TestMergedHistoryOrderingIsStable(t *testing.T) {
	gh := newRecordingGitHub()
	gh.prsByHead["a"] = &github.PullRequest{Number: 1, Head: "a"}
	history := []github.MergedPRInfo{{Bookmark: "z", PRNumber: 9}, {Bookmark: "c", PRNumber: 3}}
	gh.comments[1] = []*github.Comment{{ID: 1, Body: github.BuildStackComment([]github.StackEntry{{Bookmark: "a", PRNumber: 1}}, "a", "main", history)}}
	analysis := &AnalysisResult{Stack: []StackBookmark{{Bookmark: jjutils.Bookmark{Name: "a"}}}}
	for i := 0; i < 20; i++ {
		got := computeMergedHistory(context.Background(), &PlanningDeps{GitHub: gh}, analysis, gh.prsByHead)
		if len(got) != 2 || got[0].PRNumber != 3 || got[1].PRNumber != 9 {
			t.Fatalf("unsorted history: %v", got)
		}
	}
}
