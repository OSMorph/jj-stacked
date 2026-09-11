package open

import (
	"context"
	"testing"

	"github.com/OSMorph/jj-stacked/internal/cmdexec"
)

func TestBrowserLaunchUsesAnArgumentWithoutShellInterpolation(t *testing.T) {
	link := "https://github.com/o/r/pull/1?test=a&b=c"
	executor := cmdexec.NewMockExecutor()
	executor.SetResponse("", "open", link)
	if err := launchBrowser(context.Background(), executor, "darwin", link); err != nil {
		t.Fatal(err)
	}
	if len(executor.Calls) != 1 || executor.Calls[0].Name != "open" || len(executor.Calls[0].Args) != 1 || executor.Calls[0].Args[0] != link {
		t.Fatalf("unexpected opener: %+v", executor.Calls)
	}
}
func TestOpenOnlyAcceptsHTTPSURLsForTheConfiguredHost(t *testing.T) {
	for _, link := range []string{"file:///tmp/a", "https://elsewhere.test/1", "javascript:alert(1)", "https://user:pass@github.com/o/r/pull/1"} {
		if validateURL(link, "github.com") == nil {
			t.Fatalf("accepted URL: %q", link)
		}
	}
	if err := validateURL("https://github.company.test/o/r/pull/1", "github.company.test"); err != nil {
		t.Fatal(err)
	}
}
