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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	repository      = "OSMorph/jj-stacked"
	latestURL       = "https://api.github.com/repos/" + repository + "/releases/latest"
	maxArchiveSize  = 100 << 20
	maxChecksumSize = 1 << 20
	maxBinarySize   = 50 << 20
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
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	if exe == "" || !filepath.IsAbs(exe) {
		return fmt.Errorf("resolve current executable: expected an absolute path, got %q", exe)
	}

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
	archive, err := download(ctx, client, archiveAsset.URL, maxArchiveSize)
	if err != nil {
		return fmt.Errorf("download %s: %w", archiveName, err)
	}
	checksums, err := download(ctx, client, checksumAsset.URL, maxChecksumSize)
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

func download(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
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
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("response is too large: %d bytes (limit %d)", resp.ContentLength, limit)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d byte limit", limit)
	}
	return data, nil
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
		if header.Size < 0 || header.Size > maxBinarySize {
			return nil, fmt.Errorf("release binary %s exceeds %d byte limit", base, maxBinarySize)
		}
		content, err := io.ReadAll(io.LimitReader(tr, maxBinarySize+1))
		if err != nil {
			return nil, err
		}
		if len(content) > maxBinarySize {
			return nil, fmt.Errorf("release binary %s exceeds %d byte limit", base, maxBinarySize)
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
		if file.UncompressedSize64 > maxBinarySize {
			return nil, fmt.Errorf("release binary %s exceeds %d byte limit", base, maxBinarySize)
		}
		r, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(io.LimitReader(r, maxBinarySize+1))
		_ = r.Close()
		if readErr != nil {
			return nil, readErr
		}
		if len(content) > maxBinarySize {
			return nil, fmt.Errorf("release binary %s exceeds %d byte limit", base, maxBinarySize)
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
	targets := []string{executable}
	executableInfo, err := os.Stat(executable)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", executable, err)
	}
	for _, name := range []string{"jj-stacked", "jjk"} {
		path := filepath.Join(dir, name)
		if path == executable {
			continue
		}
		info, statErr := os.Stat(path)
		if statErr == nil && (os.SameFile(executableInfo, info) || filesEqual(executable, path)) {
			targets = append(targets, path)
		}
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
			removeStaged(prepared)
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
			removeStaged(prepared)
			return fmt.Errorf("stage %s: %w", target, err)
		}
		backup, err := reserveBackupPath(dir)
		if err != nil {
			_ = os.Remove(staged)
			removeStaged(prepared)
			return fmt.Errorf("reserve backup for %s: %w", target, err)
		}
		prepared = append(prepared, replacement{target: target, staged: staged, backup: backup})
	}

	completed := 0
	for i := range prepared {
		if err := os.Rename(prepared[i].target, prepared[i].backup); err != nil {
			return errors.Join(fmt.Errorf("back up %s: %w", prepared[i].target, err), rollbackReplacements(prepared, completed))
		}
		if err := os.Rename(prepared[i].staged, prepared[i].target); err != nil {
			restoreErr := os.Rename(prepared[i].backup, prepared[i].target)
			return errors.Join(fmt.Errorf("replace %s: %w", prepared[i].target, err), restoreErr, rollbackReplacements(prepared, completed))
		}
		completed++
	}
	for _, item := range prepared {
		_ = os.Remove(item.backup)
	}
	return nil
}

func removeStaged(items []replacement) {
	for _, item := range items {
		_ = os.Remove(item.staged)
	}
}

func rollbackReplacements(items []replacement, completed int) error {
	var rollbackErrs []error
	for i := completed - 1; i >= 0; i-- {
		if err := os.Remove(items[i].target); err != nil && !os.IsNotExist(err) {
			rollbackErrs = append(rollbackErrs, err)
		}
		if err := os.Rename(items[i].backup, items[i].target); err != nil {
			rollbackErrs = append(rollbackErrs, err)
		}
	}
	for _, item := range items {
		_ = os.Remove(item.staged)
	}
	return errors.Join(rollbackErrs...)
}

func reserveBackupPath(dir string) (string, error) {
	file, err := os.CreateTemp(dir, ".jj-stacked-backup-*")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func filesEqual(first, second string) bool {
	a, err := os.ReadFile(first)
	if err != nil {
		return false
	}
	b, err := os.ReadFile(second)
	return err == nil && bytes.Equal(a, b)
}

func isHomebrewPath(path string) bool {
	path = strings.ToLower(filepath.ToSlash(path))
	return strings.Contains(path, "/cellar/")
}

func compareVersions(a, b string) int {
	type parsedVersion struct {
		parts [3]int
		pre   string
	}
	parse := func(value string) parsedVersion {
		value = strings.TrimPrefix(strings.TrimSpace(value), "v")
		value = strings.SplitN(value, "+", 2)[0]
		var pre string
		if pieces := strings.SplitN(value, "-", 2); len(pieces) == 2 {
			value, pre = pieces[0], pieces[1]
		}
		parts := strings.Split(value, ".")
		result := parsedVersion{pre: pre}
		for i := 0; i < len(parts) && i < 3; i++ {
			_, _ = fmt.Sscanf(parts[i], "%d", &result.parts[i])
		}
		return result
	}
	if a == "dev" || a == "" {
		return -1
	}
	av, bv := parse(a), parse(b)
	for i := range av.parts {
		if av.parts[i] < bv.parts[i] {
			return -1
		}
		if av.parts[i] > bv.parts[i] {
			return 1
		}
	}
	if av.pre == bv.pre {
		return 0
	}
	if av.pre == "" {
		return 1
	}
	if bv.pre == "" {
		return -1
	}
	return comparePrerelease(av.pre, bv.pre)
}

func comparePrerelease(a, b string) int {
	aParts, bParts := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		if aParts[i] == bParts[i] {
			continue
		}
		aNum, aErr := strconv.Atoi(aParts[i])
		bNum, bErr := strconv.Atoi(bParts[i])
		switch {
		case aErr == nil && bErr == nil:
			if aNum < bNum {
				return -1
			}
			return 1
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		case aParts[i] < bParts[i]:
			return -1
		default:
			return 1
		}
	}
	if len(aParts) < len(bParts) {
		return -1
	}
	if len(aParts) > len(bParts) {
		return 1
	}
	return 0
}
