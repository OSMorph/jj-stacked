package github

import (
	"fmt"
	"regexp"
	"strings"
)

// RemoteInfo contains parsed remote URL information.
type RemoteInfo struct {
	Host  string // "github.com" or GHE hostname
	Owner string
	Repo  string
}

// AIDEV-NOTE: These patterns support both github.com and GitHub Enterprise URLs.
var (
	// SSH format: git@host:owner/repo.git
	sshPattern = regexp.MustCompile(`^git@([^:]+):([^/]+)/(.+?)(?:\.git)?$`)

	// SSH URL format: ssh://git@host/owner/repo.git or ssh://git@host:port/owner/repo.git
	sshURLPattern = regexp.MustCompile(`^ssh://git@([^/:]+)(?::\d+)?/([^/]+)/(.+?)(?:\.git)?$`)

	// HTTPS format: https://host/owner/repo.git
	httpsPattern = regexp.MustCompile(`^https?://([^/]+)/([^/]+)/(.+?)(?:\.git)?$`)
)

// ParseGitHubRemote extracts host/owner/repo from a remote URL.
// Supports both GitHub.com and GitHub Enterprise instances.
func ParseGitHubRemote(url string) (*RemoteInfo, error) {
	url = strings.TrimSpace(url)

	// Try SSH format: git@host:owner/repo.git
	if matches := sshPattern.FindStringSubmatch(url); matches != nil {
		return &RemoteInfo{
			Host:  normalizeHost(matches[1]),
			Owner: matches[2],
			Repo:  stripGitSuffix(matches[3]),
		}, nil
	}

	// Try SSH URL format: ssh://git@host/owner/repo.git or ssh://git@host:port/owner/repo.git
	if matches := sshURLPattern.FindStringSubmatch(url); matches != nil {
		return &RemoteInfo{
			Host:  normalizeHost(matches[1]),
			Owner: matches[2],
			Repo:  stripGitSuffix(matches[3]),
		}, nil
	}

	// Try HTTPS format: https://host/owner/repo.git
	if matches := httpsPattern.FindStringSubmatch(url); matches != nil {
		host := matches[1]
		// Strip authentication if present (user:pass@host)
		if atIdx := strings.LastIndex(host, "@"); atIdx != -1 {
			host = host[atIdx+1:]
		}
		return &RemoteInfo{
			Host:  normalizeHost(host),
			Owner: matches[2],
			Repo:  stripGitSuffix(matches[3]),
		}, nil
	}

	return nil, fmt.Errorf("could not parse GitHub remote URL: %s", url)
}

// stripGitSuffix removes .git suffix if present.
func stripGitSuffix(s string) string {
	return strings.TrimSuffix(s, ".git")
}

// normalizeHost normalizes the hostname (lowercase, strip port).
func normalizeHost(host string) string {
	host = strings.ToLower(host)
	// Strip port if present
	if colonIdx := strings.LastIndex(host, ":"); colonIdx != -1 {
		// Make sure it's actually a port (not part of IPv6)
		if !strings.Contains(host[colonIdx:], "]") {
			host = host[:colonIdx]
		}
	}
	return host
}

// IsGitHubRemote checks if a URL points to GitHub.com.
func IsGitHubRemote(url string) bool {
	info, err := ParseGitHubRemote(url)
	if err != nil {
		return false
	}
	return info.Host == "github.com"
}

// IsGitHubEnterpriseRemote checks if a URL points to a GitHub Enterprise instance.
// If knownGHEHosts is provided, only those hosts are considered GHE.
// Otherwise, any non-github.com host is assumed to be GHE.
func IsGitHubEnterpriseRemote(url string, knownGHEHosts []string) bool {
	info, err := ParseGitHubRemote(url)
	if err != nil {
		return false
	}

	// github.com is never GHE
	if info.Host == "github.com" {
		return false
	}

	// If we have a known hosts list, check against it
	if len(knownGHEHosts) > 0 {
		for _, known := range knownGHEHosts {
			if strings.EqualFold(info.Host, known) {
				return true
			}
		}
		return false
	}

	// Without a known hosts list, assume any non-github.com is GHE
	return true
}

// GetAPIBaseURL returns the API base URL for a given GitHub host.
func GetAPIBaseURL(host string) string {
	host = strings.ToLower(host)
	if host == "github.com" {
		return "https://api.github.com"
	}
	return fmt.Sprintf("https://%s/api/v3", host)
}
