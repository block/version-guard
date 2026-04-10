package endoflife

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

const (
	// BaseURL is the base URL for endoflife.date API
	BaseURL = "https://endoflife.date/api"

	// DefaultTimeout for HTTP requests
	DefaultTimeout = 10 * time.Second
)

// Client defines the interface for endoflife.date API calls
// This allows us to mock the HTTP client for testing
type Client interface {
	// GetProductCycles retrieves all lifecycle cycles for a product
	GetProductCycles(ctx context.Context, product string) ([]*ProductCycle, error)
}

// ProductCycle represents a single version/cycle from endoflife.date API
// API docs: https://endoflife.date/docs/api/
type ProductCycle struct {
	Cycle             string `json:"cycle"`             // Version identifier (e.g., "1.31")
	ReleaseDate       string `json:"releaseDate"`       // Release date (YYYY-MM-DD)
	Support           string `json:"support"`           // End of standard support (YYYY-MM-DD or boolean)
	EOL               string `json:"eol"`               // End of life date (YYYY-MM-DD or boolean)
	ExtendedSupport   any    `json:"extendedSupport"`   // Extended support availability (boolean or date)
	LTS               bool   `json:"lts"`               // Long-term support flag
	Latest            string `json:"latest"`            // Latest patch version
	LatestReleaseDate string `json:"latestReleaseDate"` // Latest patch release date
}

// RealHTTPClient is the production implementation of Client using net/http
type RealHTTPClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewRealHTTPClient creates a new real HTTP client for endoflife.date API
func NewRealHTTPClient() *RealHTTPClient {
	return &RealHTTPClient{
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		baseURL: BaseURL,
	}
}

// NewRealHTTPClientWithConfig creates a new client with custom configuration
func NewRealHTTPClientWithConfig(httpClient *http.Client, baseURL string) *RealHTTPClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}
	if baseURL == "" {
		baseURL = BaseURL
	}
	return &RealHTTPClient{
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

// GetProductCycles retrieves all lifecycle cycles for a product from endoflife.date API
func (c *RealHTTPClient) GetProductCycles(ctx context.Context, product string) ([]*ProductCycle, error) {
	url := fmt.Sprintf("%s/%s.json", c.baseURL, product)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}

	// Set user agent for attribution
	req.Header.Set("User-Agent", "version-guard/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch data from %s", url)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, errors.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var cycles []*ProductCycle
	if err := json.NewDecoder(resp.Body).Decode(&cycles); err != nil {
		return nil, errors.Wrap(err, "failed to decode response")
	}

	return cycles, nil
}
