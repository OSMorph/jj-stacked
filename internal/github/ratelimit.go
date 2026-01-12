package github

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/google/go-github/v67/github"

	"github.com/OSMorph/jj-stacked/internal/logger"
)

// AIDEV-NOTE: This file implements rate limit handling for GitHub API calls.
// Uses exponential backoff for retries and respects rate limit reset times.

const (
	// DefaultMaxRetries is the default maximum number of retries.
	DefaultMaxRetries = 3

	// DefaultMaxWait is the default maximum time to wait for rate limit reset.
	DefaultMaxWait = 5 * time.Minute

	// DefaultBaseBackoff is the base delay for exponential backoff.
	DefaultBaseBackoff = time.Second
)

// RateLimitConfig configures rate limit handling.
type RateLimitConfig struct {
	MaxRetries  int
	MaxWait     time.Duration
	BaseBackoff time.Duration
	Logger      *logger.Logger
}

// DefaultRateLimitConfig returns the default rate limit configuration.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		MaxRetries:  DefaultMaxRetries,
		MaxWait:     DefaultMaxWait,
		BaseBackoff: DefaultBaseBackoff,
	}
}

// RateLimitError indicates that the rate limit was exceeded.
type RateLimitError struct {
	ResetTime time.Time
	Message   string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded, resets at %s: %s", e.ResetTime.Format(time.RFC3339), e.Message)
}

// DoWithRetry executes a function with rate limit handling and retries.
// The function should return the GitHub response and any error.
func DoWithRetry(
	ctx context.Context,
	cfg RateLimitConfig,
	fn func() (*github.Response, error),
) error {
	log := cfg.Logger
	if log == nil {
		log = logger.NewFromEnv()
	}

	var lastErr error

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			// Calculate backoff delay
			backoff := cfg.BaseBackoff * time.Duration(math.Pow(2, float64(attempt-1)))
			log.Debug("retrying after backoff", "attempt", attempt, "backoff", backoff)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		resp, err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if it's a rate limit error
		if resp != nil && resp.StatusCode == http.StatusForbidden {
			if resp.Rate.Remaining == 0 {
				resetTime := resp.Rate.Reset.Time
				waitDuration := time.Until(resetTime)

				if waitDuration > cfg.MaxWait {
					return &RateLimitError{
						ResetTime: resetTime,
						Message:   fmt.Sprintf("rate limit reset time (%s) exceeds max wait (%s)", waitDuration, cfg.MaxWait),
					}
				}

				log.Info("rate limited, waiting for reset",
					"reset_time", resetTime.Format(time.RFC3339),
					"wait_duration", waitDuration,
				)

				if err := waitForRateLimit(ctx, resetTime); err != nil {
					return err
				}

				continue
			}
		}

		// Check for retryable errors
		if !isRetryable(resp, err) {
			return err
		}
	}

	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

// waitForRateLimit blocks until the rate limit resets or context is cancelled.
func waitForRateLimit(ctx context.Context, resetTime time.Time) error {
	waitDuration := time.Until(resetTime)
	if waitDuration <= 0 {
		return nil
	}

	// Add a small buffer
	waitDuration += time.Second

	timer := time.NewTimer(waitDuration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// isRetryable determines if an error is retryable.
func isRetryable(resp *github.Response, err error) bool {
	if resp == nil {
		return false
	}

	// Retry on server errors
	if resp.StatusCode >= 500 {
		return true
	}

	// Retry on rate limit (handled separately, but keep for safety)
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}

	return false
}

// CheckRateLimit checks the current rate limit status.
func CheckRateLimit(ctx context.Context, client *github.Client) (*github.RateLimits, error) {
	limits, _, err := client.RateLimit.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get rate limit: %w", err)
	}
	return limits, nil
}
