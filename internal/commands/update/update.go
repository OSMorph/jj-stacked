// Package update implements release checks and self-updates.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	repository = "OSMorph/jj-stacked"
	latestURL  = "https://api.github.com/repos/" + repository + "/releases/latest"
)

// Options configures the update command.
type Options struct {
	CurrentVersion string
	GoInstall      bool
	HTTPClient     *http.Client
	Executable     func() (string, error)
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type release struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []releaseAsset `json:"assets"`
}

type replacement struct {
	target string
	staged string
	backup string
}

// NewCommand creates the update command.
func NewCommand(opts Options) *cobra.Command {
	var checkOnly bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update jj-stacked to the latest stable release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd.Context(), opts, checkOnly)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "Check for an update without installing it")
	return cmd
}

func run(ctx context.Context, opts Options, checkOnly bool) error {
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	rel, err := fetchRelease(ctx, client)
	if err != nil {
		return err
	}
	if compareVersions(opts.CurrentVersion, rel.TagName) >= 0 {
		fmt.Printf("jj-stacked %s is already up to date.\n", opts.CurrentVersion)
		return nil
	}

	fmt.Printf("Update available: %s -> %s\n", opts.CurrentVersion, rel.TagName)
	if checkOnly {
		fmt.Printf("Release: %s\n", rel.HTMLURL)
		return nil
	}

	executable := opts.Executable
	if executable == nil {
		executable = os.Executable
	}
	exe, err := executable()
	if err != nil {
		return fmt.Errorf("find current executable: %w", err)
	}
	exe, _ = filepath.EvalSymlinks(exe)

	if isHomebrewPath(exe) {
		fmt.Println("This installation is managed by Homebrew. Run:")
		fmt.Println("  brew update && brew upgrade jj-stacked")
		return nil
	}
	if opts.GoInstall {
		fmt.Println("This installation is managed by Go. Run:")
		fmt.Printf("  go install github.com/%s/cmd/jj-stacked@latest\n", repository)
		return nil
	}

	archiveName := assetName(rel.TagName, runtime.GOOS, runtime.GOARCH)
	archiveAsset, ok := findAsset(rel.Assets, archiveName)
	if !ok {
		return fmt.Errorf("release %s has no asset for %s/%s", rel.TagName, runtime.GOOS, runtime.GOARCH)
	}
	if runtime.GOOS == "windows" {
		fmt.Printf("Automatic replacement is not supported on Windows yet. Download:\n%s\n", archiveAsset.URL)
		return nil
	}

	checksumAsset, ok := findAsset(rel.Assets, "checksums.txt")
	if !ok {
		return fmt.Errorf("release %s does not include checksums.txt", rel.TagName)
	}
	archive, err := download(ctx, client, archiveAsset.URL)
	if err != nil {
		return fmt.Errorf("download %s: %w", archiveName, err)
	}
	checksums, err := download(ctx, client, checksumAsset.URL)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	if err := verifyChecksum(archiveName, archive, checksums); err != nil {
		return err
	}

	binaries, err := extractBinaries(archiveName, archive)
	if err != nil {
		return err
	}
	if err := replaceInstalledBinaries(exe, binaries); err != nil {
		return err
	}
	fmt.Printf("Updated jj-stacked to %s.\n", rel.TagName)
	return nil
}

func fetchRelease(ctx context.Context, client *http.Client) (*release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "jj-stacked-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("check latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("check latest release: GitHub returned %s", resp.Status)
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode latest release: %w", err)
	}
	return &rel, nil
}

func download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "jj-stacked-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func assetName(version, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("jj-stacked_%s_%s_%s%s", strings.TrimPrefix(version, "v"), goos, goarch, ext)
}

func findAsset(assets []releaseAsset, name string) (releaseAsset, bool) {
	for _, asset := range assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

func verifyChecksum(name string, data, checksums []byte) error {
	var expected string
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.TrimPrefix(fields[len(fields)-1], "*") == name {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksum for %s not found", name)
	}
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(expected, actual) {
		return fmt.Errorf("checksum verification failed for %s", name)
	}
	return nil
}

func extractBinaries(name string, data []byte) (map[string][]byte, error) {
	if strings.HasSuffix(name, ".zip") {
		return extractZip(data)
	}
	return extractTarGz(data)
}

func extractTarGz(data []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open release archive: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	result := make(map[string][]byte)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read release archive: %w", err)
		}
		base := filepath.Base(header.Name)
		if base != "jj-stacked" && base != "jjk" {
			continue
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		result[base] = content
	}
	return requireBinaries(result)
}

func extractZip(data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open release archive: %w", err)
	}
	result := make(map[string][]byte)
	for _, file := range zr.File {
		base := strings.TrimSuffix(filepath.Base(file.Name), ".exe")
		if base != "jj-stacked" && base != "jjk" {
			continue
		}
		r, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(r)
		_ = r.Close()
		if readErr != nil {
			return nil, readErr
		}
		result[base] = content
	}
	return requireBinaries(result)
}

func requireBinaries(binaries map[string][]byte) (map[string][]byte, error) {
	if len(binaries["jj-stacked"]) == 0 || len(binaries["jjk"]) == 0 {
		return nil, fmt.Errorf("release archive is missing jj-stacked or jjk")
	}
	return binaries, nil
}

func replaceInstalledBinaries(executable string, binaries map[string][]byte) error {
	dir := filepath.Dir(executable)
	targets := make([]string, 0, 2)
	for _, name := range []string{"jj-stacked", "jjk"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil || path == executable {
			targets = append(targets, path)
		}
	}
	if len(targets) == 0 {
		targets = append(targets, executable)
	}
	sort.Strings(targets)

	prepared := make([]replacement, 0, len(targets))
	for _, target := range targets {
		name := strings.TrimSuffix(filepath.Base(target), ".exe")
		content := binaries[name]
		if len(content) == 0 {
			content = binaries["jj-stacked"]
		}
		info, err := os.Stat(target)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", target, err)
		}
		file, err := os.CreateTemp(dir, ".jj-stacked-update-*")
		if err != nil {
			return fmt.Errorf("stage update in %s: %w", dir, err)
		}
		staged := file.Name()
		if _, err = file.Write(content); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Chmod(staged, info.Mode().Perm())
		}
		if err != nil {
			_ = os.Remove(staged)
			return fmt.Errorf("stage %s: %w", target, err)
		}
		prepared = append(prepared, replacement{target: target, staged: staged, backup: target + ".update-backup"})
	}

	completed := 0
	for i := range prepared {
		_ = os.Remove(prepared[i].backup)
		if err := os.Rename(prepared[i].target, prepared[i].backup); err != nil {
			rollbackReplacements(prepared, completed)
			return fmt.Errorf("back up %s: %w", prepared[i].target, err)
		}
		if err := os.Rename(prepared[i].staged, prepared[i].target); err != nil {
			_ = os.Rename(prepared[i].backup, prepared[i].target)
			rollbackReplacements(prepared, completed)
			return fmt.Errorf("replace %s: %w", prepared[i].target, err)
		}
		completed++
	}
	for _, item := range prepared {
		_ = os.Remove(item.backup)
	}
	return nil
}

func rollbackReplacements(items []replacement, completed int) {
	for i := completed - 1; i >= 0; i-- {
		_ = os.Remove(items[i].target)
		_ = os.Rename(items[i].backup, items[i].target)
	}
	for _, item := range items {
		_ = os.Remove(item.staged)
	}
}

func isHomebrewPath(path string) bool {
	path = strings.ToLower(filepath.ToSlash(path))
	return strings.Contains(path, "/cellar/") || strings.Contains(path, "/homebrew/")
}

func compareVersions(a, b string) int {
	parse := func(value string) []int {
		value = strings.TrimPrefix(strings.TrimSpace(value), "v")
		value = strings.SplitN(value, "-", 2)[0]
		parts := strings.Split(value, ".")
		result := make([]int, 3)
		for i := 0; i < len(parts) && i < 3; i++ {
			_, _ = fmt.Sscanf(parts[i], "%d", &result[i])
		}
		return result
	}
	if a == "dev" || a == "" {
		return -1
	}
	av, bv := parse(a), parse(b)
	for i := range av {
		if av[i] < bv[i] {
			return -1
		}
		if av[i] > bv[i] {
			return 1
		}
	}
	return 0
}
