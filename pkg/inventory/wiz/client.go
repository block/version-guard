package wiz

import (
	"context"
	"encoding/csv"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/sync/singleflight"
)

const (
	// DefaultCacheTTL is the default time to cache downloaded reports
	DefaultCacheTTL = time.Hour
)

// WizClient defines the interface for interacting with Wiz API
// This allows us to mock the client for testing
//
//nolint:revive // WizClient is intentionally verbose for clarity
type WizClient interface {
	// GetAccessToken retrieves an access token for Wiz API
	GetAccessToken(ctx context.Context) (string, error)

	// GetReport retrieves report metadata including download URL
	GetReport(ctx context.Context, accessToken, reportID string) (*Report, error)

	// DownloadReport downloads the report CSV from the provided URL
	DownloadReport(ctx context.Context, url string) (io.ReadCloser, error)
}

//nolint:govet // field alignment sacrificed for readability
type Report struct {
	ID               string
	Name             string
	DownloadURL      string
	LastRun          time.Time
	RunIntervalHours int
	ExpectedRows     int
}

// Client wraps the Wiz API client with caching and CSV parsing.
//
// The cache is per-reportID — Version-Guard fans out one detection
// workflow per resource type and they all share a single *Client (see
// cmd/server/main.go). A single-slot cache evicts on every reportID
// switch, dropping the effective hit rate to ~0% during parallel
// scans; per-key caching plus a singleflight group lets concurrent
// callers for the *same* reportID collapse onto one HTTP fetch
// without serializing fetches across *different* reportIDs.
//
//nolint:govet // field alignment sacrificed for readability
type Client struct {
	mu        sync.RWMutex
	cache     map[string]*cachedReport // key: reportID
	wizClient WizClient
	cacheTTL  time.Duration
	group     singleflight.Group
}

//nolint:govet // field alignment sacrificed for readability
type cachedReport struct {
	data      [][]string
	fetchedAt time.Time
}

// NewClient creates a new Wiz client with caching
func NewClient(wizClient WizClient, cacheTTL time.Duration) *Client {
	if cacheTTL == 0 {
		cacheTTL = DefaultCacheTTL
	}

	return &Client{
		wizClient: wizClient,
		cacheTTL:  cacheTTL,
		cache:     make(map[string]*cachedReport),
	}
}

// GetReportData fetches and parses a Wiz saved report, returning CSV
// rows. Each reportID is cached independently for cacheTTL duration so
// parallel scans across different resource types don't evict each
// other's data.
func (c *Client) GetReportData(ctx context.Context, reportID string) ([][]string, error) {
	// Fast path: read-locked cache lookup.
	if rows, ok := c.lookup(reportID); ok {
		return rows, nil
	}

	// Cache miss or expired — singleflight collapses concurrent
	// fetches for the same reportID onto a single HTTP call. Different
	// reportIDs proceed in parallel because singleflight keys on
	// reportID and the HTTP fetch itself holds no lock.
	result, err, _ := c.group.Do(reportID, func() (interface{}, error) {
		// Re-check the cache inside the singleflight slot — a previous
		// caller in the same flight may have already populated it.
		if rows, ok := c.lookup(reportID); ok {
			return rows, nil
		}
		return c.fetchAndCache(ctx, reportID)
	})
	if err != nil {
		return nil, err
	}
	rows, ok := result.([][]string)
	if !ok {
		return nil, errors.Errorf("wiz cache returned unexpected type for report %s", reportID)
	}
	return rows, nil
}

// lookup returns cached rows for reportID if still within TTL.
func (c *Client) lookup(reportID string) ([][]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cached, ok := c.cache[reportID]
	if !ok {
		return nil, false
	}
	if time.Since(cached.fetchedAt) >= c.cacheTTL {
		return nil, false
	}
	return cached.data, true
}

// fetchAndCache performs the GraphQL + download + CSV-parse pipeline
// for one reportID and writes the parsed rows into the cache before
// returning. Called from inside a singleflight slot, so at most one
// goroutine per reportID is here at a time.
func (c *Client) fetchAndCache(ctx context.Context, reportID string) ([][]string, error) {
	accessToken, err := c.wizClient.GetAccessToken(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get Wiz access token")
	}

	report, err := c.wizClient.GetReport(ctx, accessToken, reportID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get Wiz report %s", reportID)
	}

	resp, err := c.wizClient.DownloadReport(ctx, report.DownloadURL)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to download Wiz report %s", reportID)
	}
	defer resp.Close()

	csvReader := csv.NewReader(resp)
	rows, err := csvReader.ReadAll()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse Wiz report CSV for report %s", reportID)
	}

	// Note: Empty reports (header only) are valid - the inventory source will filter them

	c.mu.Lock()
	c.cache[reportID] = &cachedReport{
		data:      rows,
		fetchedAt: time.Now(),
	}
	c.mu.Unlock()

	return rows, nil
}

// ParseTags extracts tags from a Wiz Tags column (JSON format)
// Example: [{"key":"app","value":"my-app"},{"key":"env","value":"prod"}]
func ParseTags(tagsJSON string) (map[string]string, error) {
	if tagsJSON == "" {
		return make(map[string]string), nil
	}

	// Simple JSON parsing for tag array
	// Format: [{"key":"k1","value":"v1"},{"key":"k2","value":"v2"}]
	tags := make(map[string]string)

	// Quick and dirty JSON parsing (avoids encoding/json for simplicity)
	// In production, you could use json.Unmarshal with proper structs

	// Remove brackets and spaces
	tagsJSON = strings.TrimSpace(tagsJSON)
	if tagsJSON == "[]" || tagsJSON == "" {
		return tags, nil
	}

	// Split by "},{"
	tagsJSON = strings.TrimPrefix(tagsJSON, "[")
	tagsJSON = strings.TrimSuffix(tagsJSON, "]")

	// Parse each tag object
	for _, tagPart := range splitTags(tagsJSON) {
		key, value := parseTagObject(tagPart)
		if key != "" {
			tags[key] = value
		}
	}

	return tags, nil
}

// splitTags splits the tags JSON into individual tag objects
func splitTags(tagsJSON string) []string {
	var tags []string
	depth := 0
	start := 0

	for i, ch := range tagsJSON {
		if ch == '{' {
			depth++
			if depth == 1 {
				start = i
			}
		} else if ch == '}' {
			depth--
			if depth == 0 {
				tags = append(tags, tagsJSON[start:i+1])
			}
		}
	}

	return tags
}

// parseTagObject extracts key and value from a tag object
// Example: {"key":"app","value":"my-app"}
func parseTagObject(tagObj string) (key, value string) {
	// Find "key": portion
	keyIdx := strings.Index(tagObj, `"key"`)
	if keyIdx >= 0 {
		// Find the value after "key":"
		start := strings.Index(tagObj[keyIdx:], `":"`) + keyIdx
		if start > keyIdx {
			start += 3 // Skip ":"
			end := strings.Index(tagObj[start:], `"`) + start
			if end > start {
				key = tagObj[start:end]
			}
		}
	}

	// Find "value": portion
	valIdx := strings.Index(tagObj, `"value"`)
	if valIdx >= 0 {
		// Find the value after "value":"
		start := strings.Index(tagObj[valIdx:], `":"`) + valIdx
		if start > valIdx {
			start += 3 // Skip ":"
			end := strings.Index(tagObj[start:], `"`) + start
			if end > start {
				value = tagObj[start:end]
			}
		}
	}

	return key, value
}
