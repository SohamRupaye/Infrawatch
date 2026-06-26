package health

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
)

// CheckResult holds the outcome of a single health check poll.
type CheckResult struct {
	ServiceName  string
	OK           bool
	StatusCode   int
	ResponseTime time.Duration
	Error        error
	Timestamp    time.Time
	// Source identifies where this result came from: "poller" for a real
	// HTTP check, "alertmanager" for a passive-mode service driven by an
	// external alert floor. Defaults to "" (treated as "poller").
	Source string
}

// Result sources.
const (
	SourcePoller       = "poller"
	SourceAlertmanager = "alertmanager"
)

// Checker performs HTTP health checks for a service.
type Checker struct {
	client *http.Client
}

// NewChecker creates a Checker with a pre-configured HTTP client.
func NewChecker() *Checker {
	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: false},
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	}
	client := &http.Client{
		Transport: transport,
		// Redirect follow is fine; we capture the final status
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	return &Checker{client: client}
}

// Check performs a single health check against svc.
// The context carries the per-check timeout.
func (c *Checker) Check(ctx context.Context, svc config.ServiceConfig) CheckResult {
	result := CheckResult{
		ServiceName: svc.Name,
		Timestamp:   time.Now(),
	}

	// Apply per-service timeout
	timeout := svc.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, svc.Method, svc.URL, nil)
	if err != nil {
		result.Error = fmt.Errorf("building request: %w", err)
		return result
	}

	// Attach configured headers
	for k, v := range svc.Headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "infrawatch-health-checker/1.0")
	}

	start := time.Now()
	resp, err := c.client.Do(req)
	elapsed := time.Since(start)
	result.ResponseTime = elapsed

	if err != nil {
		result.Error = err
		result.OK = false
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	expected := svc.ExpectStatus
	if expected == 0 {
		expected = 200
	}

	if resp.StatusCode != expected {
		result.OK = false
		result.Error = fmt.Errorf("expected status %d, got %d", expected, resp.StatusCode)
		return result
	}

	// Optional body matching
	if svc.ExpectBody != "" {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // Limit to 1MB
		if err != nil {
			result.OK = false
			result.Error = fmt.Errorf("reading response body: %w", err)
			return result
		}
		if !containsString(string(body), svc.ExpectBody) {
			result.OK = false
			result.Error = fmt.Errorf("body missing expected string: %s", svc.ExpectBody)
			return result
		}
	} else {
		// Drain body to allow connection reuse if we didn't read it for ExpectBody
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	}

	result.OK = true
	return result
}

func containsString(body, target string) bool {
	return strings.Contains(body, target)
}
