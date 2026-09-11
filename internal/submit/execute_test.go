package submit

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/github"
	"github.com/OSMorph/jj-stacked/internal/jjutils"
)

type recordingGitHub struct {
	*fakeGitHubClient
	events               []string
	bodies               map[int]string
	createErr, updateErr error
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
	g.events = append(g.events, "update")
	if g.updateErr != nil {
		return nil, g.updateErr
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
	events *[]string
	err    error
}

func (j *recordingJJ) Push(context.Context, string, string) error {
	*j.events = append(*j.events, "push")
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

func TestExecuteSubmissionStopsCriticalFailuresAndContinuesMetadataFailures(t *testing.T) {
	for _, fail := range []string{"push", "create", "update"} {
		t.Run(fail, func(t *testing.T) {
			gh := newRecordingGitHub()
			jj := &recordingJJ{events: &gh.events}
			failure := errors.New("fixture failure")
			switch fail {
			case "push":
				jj.err = failure
			case "create":
				gh.createErr = failure
			case "update":
				gh.updateErr = failure
			}
			plan := &SubmissionPlan{Actions: []SubmissionAction{&PushAction{Bookmark: "a"}, &CreatePRAction{Bookmark: "a"}, &UpdateBaseAction{PRNumber: 1, NewBase: "main"}, &SyncCommentAction{Bookmark: "a", PRNumber: 1, BaseBranch: "main"}}}
			result, err := ExecuteSubmissionPlan(context.Background(), plan, &ActionDeps{GitHub: gh, JJ: jj}, nil)
			if result.Summary.Failed != 1 {
				t.Fatalf("wrong failure count: %+v", result)
			}
			if fail == "update" {
				if err != nil || len(gh.events) != 4 {
					t.Fatalf("metadata failure stopped execution: %v %v", gh.events, err)
				}
			} else {
				if !errors.Is(err, failure) || result.Summary.Skipped == 0 {
					t.Fatalf("critical failure continued: %+v %v", result, err)
				}
				for _, event := range gh.events {
					if event == "comment:create" {
						t.Fatal("comment followed critical failure")
					}
				}
			}
		})
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
