package common

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestDebugFlagPlacementAndEnvironment(t *testing.T) {
	for _, tc := range []struct {
		args []string
		env  string
		want bool
	}{
		{[]string{"--debug", "child"}, "", true},
		{[]string{"child", "--debug"}, "", true},
		{[]string{"child"}, "1", true},
		{[]string{"child", "--debug=false"}, "1", false},
	} {
		t.Run(strings.Join(tc.args, " ")+tc.env, func(t *testing.T) {
			t.Setenv("JJ_STACK_DEBUG", tc.env)
			root := &cobra.Command{Use: "test"}
			root.PersistentFlags().Bool("debug", false, "")
			root.AddCommand(&cobra.Command{Use: "child", Run: func(cmd *cobra.Command, _ []string) {
				if got := Debug(cmd); got != tc.want {
					t.Fatalf("debug=%v want %v", got, tc.want)
				}
			}})
			root.SetArgs(tc.args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPromptEOFDoesNotApprove(t *testing.T) {
	for _, text := range []string{"", "yes"} {
		prompt := NewPrompt(strings.NewReader(text), io.Discard)
		confirmed, err := prompt.Confirm("Proceed?")
		if err == nil || confirmed {
			t.Fatalf("EOF approved: %v %v", confirmed, err)
		}
	}
	var output bytes.Buffer
	prompt := NewPrompt(strings.NewReader("2,1,2\nno\n"), &output)
	indices, err := prompt.Select(2, true)
	if err != nil || len(indices) != 2 {
		t.Fatalf("selection %v %v", indices, err)
	}
	confirmed, err := prompt.Confirm("Proceed?")
	if err != nil || confirmed {
		t.Fatalf("next answer consumed incorrectly: %v %v", confirmed, err)
	}
}
