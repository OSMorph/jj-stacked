package jjutils

import (
	"context"
	"reflect"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
)

func TestPushTracksBookmarkOnCurrentJJ(t *testing.T) {
	executor := cmdexec.NewMockExecutor()
	executor.SetResponse("jj 0.44.0\n", "jj", "--version")
	executor.SetResponse("", "jj", "bookmark", "track", "feature", "--remote", "upstream")
	executor.SetResponse("", "jj", "git", "push", "--remote", "upstream", "--bookmark", "feature")

	jj := NewJJFunctions(executor, "jj")
	err := jj.Push(context.Background(), "upstream", "feature")
	if err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"--version"},
		{"bookmark", "track", "feature", "--remote", "upstream"},
		{"git", "push", "--remote", "upstream", "--bookmark", "feature"},
	}
	assertCommandArgs(t, executor.Calls, want)
}

func TestPushUsesAllowNewForOlderSupportedJJ(t *testing.T) {
	executor := cmdexec.NewMockExecutor()
	executor.SetResponse("jj 0.27.0-abcdef\n", "jj", "--version")
	executor.SetResponse("", "jj", "git", "push", "--remote", "origin", "--bookmark", "feature", "--allow-new")

	jj := NewJJFunctions(executor, "jj")
	err := jj.Push(context.Background(), "origin", "feature")
	if err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"--version"},
		{"git", "push", "--remote", "origin", "--bookmark", "feature", "--allow-new"},
	}
	assertCommandArgs(t, executor.Calls, want)
}

func TestPushUsesLegacyTrackingSyntaxForJJ036(t *testing.T) {
	executor := cmdexec.NewMockExecutor()
	executor.SetResponse("jj 0.36.0\n", "jj", "--version")
	executor.SetResponse("", "jj", "bookmark", "track", "feature@origin")
	executor.SetResponse("", "jj", "git", "push", "--remote", "origin", "--bookmark", "feature")

	jj := NewJJFunctions(executor, "jj")
	err := jj.Push(context.Background(), "origin", "feature")
	if err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"--version"},
		{"bookmark", "track", "feature@origin"},
		{"git", "push", "--remote", "origin", "--bookmark", "feature"},
	}
	assertCommandArgs(t, executor.Calls, want)
}

func TestParseJJVersion(t *testing.T) {
	tests := []struct {
		output string
		want   jjVersion
	}{
		{output: "jj 0.36.0\n", want: jjVersion{major: 0, minor: 36}},
		{output: "jj 0.44.0-a1b2c3\n", want: jjVersion{major: 0, minor: 44}},
		{output: "Jujutsu 1.2.3\n", want: jjVersion{major: 1, minor: 2}},
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			executor := cmdexec.NewMockExecutor()
			executor.SetResponse(tt.output, "jj", "--version")
			jj := NewJJFunctions(executor, "jj").(*jjFunctions)

			got, err := jj.getVersion(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("version = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func assertCommandArgs(t *testing.T, calls []cmdexec.MockCall, want [][]string) {
	t.Helper()
	if len(calls) != len(want) {
		t.Fatalf("calls = %d, want %d: %#v", len(calls), len(want), calls)
	}
	for i := range want {
		if calls[i].Name != "jj" || !reflect.DeepEqual(calls[i].Args, want[i]) {
			t.Errorf("call[%d] = %s %v, want jj %v", i, calls[i].Name, calls[i].Args, want[i])
		}
	}
}
