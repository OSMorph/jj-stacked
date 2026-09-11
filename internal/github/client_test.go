package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFindPRByHeadAllStatesNormalizesListResponses(t *testing.T) {
	for _, tc := range []struct {
		name, state, mergedAt string
		merged                bool
	}{
		{"merged", "closed", `"2026-09-04T10:00:00Z"`, true},
		{"closed without merge", "closed", "null", false},
		{"open", "open", "null", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			head := strings.Repeat("a", 40)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/repos/o/r/pulls" || r.URL.Query().Get("state") != "all" || r.URL.Query().Get("head") != "o:feature" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				// The list API supplies merged_at but need not supply merged.
				_, _ = fmt.Fprintf(w, `[{"number":12,"state":%q,"merged_at":%s,"merge_commit_sha":%q,"head":{"ref":"feature","sha":%q},"base":{"ref":"main"}}]`, tc.state, tc.mergedAt, strings.Repeat("b", 40), head)
			}))
			defer server.Close()
			client, err := NewClient(ClientOptions{Token: "fixture", APIBaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			pr, err := client.FindPRByHeadAllStates(context.Background(), "o", "r", "feature")
			if err != nil {
				t.Fatal(err)
			}
			if pr.Merged != tc.merged || pr.HeadSHA != head || pr.MergeCommitSHA != strings.Repeat("b", 40) || pr.MatchesMergedHead(head) != tc.merged {
				t.Fatalf("unexpected normalized PR: %+v", pr)
			}
			if pr.MatchesMergedHead(head[:12]) || pr.MatchesMergedHead(strings.Repeat("b", 40)) {
				t.Fatal("merged match accepted abbreviated or changed head")
			}
		})
	}
}

func TestNewClientRejectsInvalidAPIBaseURL(t *testing.T) {
	for _, base := range []string{"relative/path", "file:///tmp/api", "://bad"} {
		if _, err := NewClient(ClientOptions{Token: "fixture", APIBaseURL: base}); err == nil {
			t.Fatalf("accepted invalid base %q", base)
		}
	}
}
