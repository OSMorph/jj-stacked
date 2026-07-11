package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v2.4.2", "v2.4.3", -1},
		{"2.4.3", "v2.4.3", 0},
		{"v2.5.0", "v2.4.3", 1},
		{"v2.5.0-rc.1", "v2.5.0", -1},
		{"v2.5.0-rc.10", "v2.5.0-rc.2", 1},
		{"v2.5.0", "v2.5.0-rc.1", 1},
		{"v2.5.0+build.2", "v2.5.0+build.1", 0},
		{"dev", "v2.4.3", -1},
	}
	for _, tt := range tests {
		if got := compareVersions(tt.a, tt.b); got != tt.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestDownloadRejectsOversizedResponse(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			ContentLength: 5,
			Body:          io.NopCloser(strings.NewReader("12345")),
		}, nil
	})}
	if _, err := download(t.Context(), client, "https://example.test/asset", 4); err == nil {
		t.Fatal("expected oversized response error")
	}
}

func TestReplaceInstalledBinariesDoesNotOverwriteUnrelatedNeighbors(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "custom-name")
	neighbor := filepath.Join(dir, "jj-stacked")
	if err := os.WriteFile(executable, []byte("running-copy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(neighbor, []byte("unrelated"), 0o755); err != nil {
		t.Fatal(err)
	}
	binaries := map[string][]byte{"jj-stacked": []byte("new-main"), "jjk": []byte("new-alias")}
	if err := replaceInstalledBinaries(executable, binaries); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(executable)
	if string(got) != "new-main" {
		t.Fatalf("executable = %q, want new-main", got)
	}
	got, _ = os.ReadFile(neighbor)
	if string(got) != "unrelated" {
		t.Fatalf("neighbor was overwritten: %q", got)
	}
}

func TestReplaceInstalledBinariesPreservesPreexistingBackup(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "jj-stacked")
	backup := executable + ".update-backup"
	if err := os.WriteFile(executable, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceInstalledBinaries(executable, map[string][]byte{"jj-stacked": []byte("new"), "jjk": []byte("new")}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(backup)
	if string(got) != "sentinel" {
		t.Fatalf("preexisting backup = %q, want sentinel", got)
	}
}

func TestVerifyChecksumExactFilename(t *testing.T) {
	data := []byte("archive")
	sum := sha256.Sum256(data)
	checksums := []byte(fmt.Sprintf("deadbeef  prefix-asset.tar.gz\n%x  asset.tar.gz\n", sum))
	if err := verifyChecksum("asset.tar.gz", data, checksums); err != nil {
		t.Fatal(err)
	}
	if err := verifyChecksum("missing.tar.gz", data, checksums); err == nil {
		t.Fatal("expected missing checksum error")
	}
}

func TestExtractAndReplaceInstalledBinaries(t *testing.T) {
	archive := makeTarGz(t, map[string]string{"jj-stacked": "new-main", "jjk": "new-alias"})
	binaries, err := extractBinaries("release.tar.gz", archive)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "jj-stacked")
	aliasPath := filepath.Join(dir, "jjk")
	for _, path := range []string{mainPath, aliasPath} {
		if err := os.WriteFile(path, []byte("old"), 0o751); err != nil {
			t.Fatal(err)
		}
	}
	if err := replaceInstalledBinaries(aliasPath, binaries); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{mainPath: "new-main", aliasPath: "new-alias"} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o751 {
			t.Errorf("%s mode = %o, want 751", path, info.Mode().Perm())
		}
	}
}

func TestAssetName(t *testing.T) {
	if got := assetName("v2.4.3", "darwin", "arm64"); got != "jj-stacked_2.4.3_darwin_arm64.tar.gz" {
		t.Fatal(got)
	}
	if got := assetName("v2.4.3", "windows", "amd64"); got != "jj-stacked_2.4.3_windows_amd64.zip" {
		t.Fatal(got)
	}
}

func TestIsHomebrewPath(t *testing.T) {
	if !isHomebrewPath("/opt/homebrew/Cellar/jj-stacked/2.4.3/bin/jjk") {
		t.Fatal("expected Homebrew Cellar path to be detected")
	}
	if isHomebrewPath("/Users/test/.local/bin/jjk") {
		t.Fatal("ordinary release path detected as Homebrew")
	}
	if isHomebrewPath("/Users/test/homebrew/bin/jjk") {
		t.Fatal("directory merely named homebrew detected as managed installation")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRunSelfUpdateEndToEnd(t *testing.T) {
	archiveName := assetName("v9.9.9", runtime.GOOS, runtime.GOARCH)
	archive := makeTarGz(t, map[string]string{"jj-stacked": "release-main", "jjk": "release-alias"})
	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%x  %s\n", sum, archiveName)
	releaseJSON := fmt.Sprintf(`{"tag_name":"v9.9.9","html_url":"https://example.test/release","assets":[{"name":%q,"browser_download_url":"https://example.test/archive"},{"name":"checksums.txt","browser_download_url":"https://example.test/checksums"}]}`, archiveName)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body []byte
		switch req.URL.String() {
		case latestURL:
			body = []byte(releaseJSON)
		case "https://example.test/archive":
			body = archive
		case "https://example.test/checksums":
			body = []byte(checksums)
		default:
			t.Fatalf("unexpected request: %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "jj-stacked")
	aliasPath := filepath.Join(dir, "jjk")
	for _, path := range []string{mainPath, aliasPath} {
		if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows intentionally prints the release URL instead of replacing the executable")
	}
	if err := run(t.Context(), Options{CurrentVersion: "v1.0.0", HTTPClient: client, Executable: func() (string, error) { return aliasPath, nil }}, false); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(mainPath)
	if !strings.Contains(string(got), "release-main") {
		t.Fatalf("main binary was not updated: %q", got)
	}
}

func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
