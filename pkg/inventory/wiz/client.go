package wiz

import (
	"context"
	"encoding/csv"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
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
	ID          string
	Name        string
	DownloadURL string
	LastRun     time.Time
}

// Client wraps the Wiz API client with caching and CSV parsing
//
//nolint:govet // field alignment sacrificed for readability
type Client struct {
	mu           sync.RWMutex
	cachedReport *cachedReport
	wizClient    WizClient
	cacheTTL     time.Duration
}

//nolint:govet // field alignment sacrificed for readability
type cachedReport struct {
	data      [][]string
	fetchedAt time.Time
	reportID  string
}

// NewClient creates a new Wiz client with caching
func NewClient(wizClient WizClient, cacheTTL time.Duration) *Client {
	if cacheTTL == 0 {
		cacheTTL = DefaultCacheTTL
	}

	return &Client{
		wizClient: wizClient,
		cacheTTL:  cacheTTL,
	}
}

// GetReportData fetches and parses a Wiz saved report, returning CSV rows
// The report is cached for cacheTTL duration to avoid excessive API calls
func (c *Client) GetReportData(ctx context.Context, reportID string) ([][]string, error) {
	// Check cache first
	c.mu.RLock()
	if c.cachedReport != nil &&
		c.cachedReport.reportID == reportID &&
		time.Since(c.cachedReport.fetchedAt) < c.cacheTTL {
		data := c.cachedReport.data
		c.mu.RUnlock()
		return data, nil
	}
	c.mu.RUnlock()

	// Cache miss or expired - fetch new data
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check in case another goroutine just fetched
	if c.cachedReport != nil &&
		c.cachedReport.reportID == reportID &&
		time.Since(c.cachedReport.fetchedAt) < c.cacheTTL {
		return c.cachedReport.data, nil
	}

	// Fetch access token
	accessToken, err := c.wizClient.GetAccessToken(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get Wiz access token")
	}

	// Get report metadata
	report, err := c.wizClient.GetReport(ctx, accessToken, reportID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get Wiz report %s", reportID)
	}

	// Download report CSV
	resp, err := c.wizClient.DownloadReport(ctx, report.DownloadURL)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to download Wiz report %s", reportID)
	}
	defer resp.Close()

	// Parse CSV
	csvReader := csv.NewReader(resp)
	rows, err := csvReader.ReadAll()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse Wiz report CSV for report %s", reportID)
	}

	// Note: Empty reports (header only) are valid - the inventory source will filter them

	// Cache the result
	c.cachedReport = &cachedReport{
		reportID:  reportID,
		data:      rows,
		fetchedAt: time.Now(),
	}

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

