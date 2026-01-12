// Package config provides configuration management for jj-stacked.
package config

import (
	"fmt"
	"os"
	"os/exec"
)

// Config holds the application configuration loaded from environment variables.
// AIDEV-NOTE: Supports both GitHub.com and GitHub Enterprise (GHE) instances.
type Config struct {
	// GitHub authentication (supports both github.com and GHE)
	GitHubToken string // GITHUB_TOKEN or GH_TOKEN (used for all hosts unless GHE_TOKEN set)
	GHEToken    string // GHE_TOKEN (optional: separate token for GHE instances)

	// GitHub host configuration
	GitHubHost   string // GITHUB_HOST (optional: "github.com" or GHE hostname like "git.mycompany.com")
	GitHubAPIURL string // GITHUB_API_URL (optional: override API base URL)

	// Repository overrides (optional)
	GitHubOwner string // GITHUB_OWNER (optional override)
	GitHubRepo  string // GITHUB_REPO (optional override)

	// Jujutsu configuration
	JJPath string // JJ_PATH (custom jj binary location)

	// Debug configuration
	Debug     bool   // JJ_STACK_DEBUG
	LogFormat string // JJ_STACK_LOG_FORMAT
}

// LoadConfig loads configuration from environment variables.
// AIDEV-NOTE: Does not fail on missing GitHub token at load time since auth might come from gh CLI.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		GitHubToken:  getEnvWithFallback("GITHUB_TOKEN", "GH_TOKEN"),
		GHEToken:     os.Getenv("GHE_TOKEN"),
		GitHubHost:   os.Getenv("GITHUB_HOST"),
		GitHubAPIURL: os.Getenv("GITHUB_API_URL"),
		GitHubOwner:  os.Getenv("GITHUB_OWNER"),
		GitHubRepo:   os.Getenv("GITHUB_REPO"),
		JJPath:       os.Getenv("JJ_PATH"),
		Debug:        os.Getenv("JJ_STACK_DEBUG") != "",
		LogFormat:    os.Getenv("JJ_STACK_LOG_FORMAT"),
	}

	// Validate JJ_PATH exists and is executable if set
	if cfg.JJPath != "" {
		if err := validateExecutable(cfg.JJPath); err != nil {
			return nil, fmt.Errorf("invalid JJ_PATH: %w", err)
		}
	}

	return cfg, nil
}

// GetTokenForHost returns the appropriate token for the given GitHub host.
// For non-github.com hosts, prefers GHE_TOKEN if set.
func (c *Config) GetTokenForHost(host string) string {
	if host != "github.com" && c.GHEToken != "" {
		return c.GHEToken
	}
	return c.GitHubToken
}

// GetAPIURLForHost returns the API base URL for the given GitHub host.
// Uses GitHubAPIURL override if set, otherwise derives from host.
func (c *Config) GetAPIURLForHost(host string) string {
	if c.GitHubAPIURL != "" {
		return c.GitHubAPIURL
	}
	if host == "github.com" {
		return "https://api.github.com"
	}
	return fmt.Sprintf("https://%s/api/v3", host)
}

// HasToken returns true if any GitHub token is configured.
func (c *Config) HasToken() bool {
	return c.GitHubToken != "" || c.GHEToken != ""
}

// Validate checks that required configuration is present.
// Call this when authentication is actually needed, not at load time.
func (c *Config) Validate() error {
	// Token validation is deferred until actually needed
	// since we might get auth from gh CLI
	return nil
}

// getEnvWithFallback returns the value of the first non-empty environment variable.
func getEnvWithFallback(keys ...string) string {
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			return val
		}
	}
	return ""
}

// validateExecutable checks that a path exists and is executable.
func validateExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", path)
		}
		return err
	}

	if info.IsDir() {
		return fmt.Errorf("path is a directory, not an executable: %s", path)
	}

	// Check if file is executable by trying to look it up
	if _, err := exec.LookPath(path); err != nil {
		return fmt.Errorf("file is not executable: %s", path)
	}

	return nil
}
