package endoflife

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/block/Version-Guard/pkg/types"
)

const (
	// BaseURL is the base URL for endoflife.date API
	BaseURL = "https://endoflife.date/api"

	// DefaultTimeout for HTTP requests
	DefaultTimeout = 10 * time.Second

	// EOLSourceHeader identifies the trusted lifecycle source selected by a proxy.
	EOLSourceHeader = "X-Version-Guard-EOL-Source"
)

// ErrProductNotFound is returned by GetProductCycles when the upstream
// API responds with 404 — i.e. the product slug is unknown to
// endoflife.date (typo, new product not yet added, or pending PR).
//
// Callers should treat this as a recoverable signal (resource type
// becomes UNKNOWN rather than the whole scan failing) and discriminate
// it via errors.Is rather than substring-matching the wrapped message,
// which would silently break if the wrapper text changes.
var ErrProductNotFound = errors.New("endoflife.date product not found")

// Client defines the interface for endoflife.date API calls
// This allows us to mock the HTTP client for testing
type Client interface {
	// GetProductCycles retrieves all lifecycle cycles for a product
	GetProductCycles(ctx context.Context, product string) (ProductCyclesResult, error)
}

// ProductCyclesResult contains lifecycle cycles and metadata about their source.
//
//nolint:govet // Field order groups the response payload before its attribution metadata.
type ProductCyclesResult struct {
	Cycles     []*ProductCycle
	FetchedAt  time.Time
	DataSource types.LifecycleDataSource
}

// ProductCycle represents a single version/cycle from endoflife.date API
// API docs: https://endoflife.date/docs/api/
type ProductCycle struct {
	Cycle             string `json:"cycle"`             // Version identifier (e.g., "1.31")
	ReleaseDate       string `json:"releaseDate"`       // Release date (YYYY-MM-DD)
	Support           any    `json:"support"`           // End of standard support (YYYY-MM-DD or boolean)
	EOL               any    `json:"eol"`               // End of life date (YYYY-MM-DD or boolean)
	ExtendedSupport   any    `json:"extendedSupport"`   // Extended support availability (boolean or date)
	LTS               any    `json:"lts"`               // Long-term support flag or start date
	Latest            string `json:"latest"`            // Latest patch version
	LatestReleaseDate string `json:"latestReleaseDate"` // Latest patch release date
}

// RealHTTPClient is the production implementation of Client using net/http
type RealHTTPClient struct {
	httpClient        *http.Client
	baseURL           string
	defaultDataSource types.LifecycleDataSource
}

// NewRealHTTPClient creates a new real HTTP client for endoflife.date API
func NewRealHTTPClient() *RealHTTPClient {
	return &RealHTTPClient{
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		baseURL:           BaseURL,
		defaultDataSource: types.LifecycleDataSourceEndOfLifeDate,
	}
}

// NewRealHTTPClientWithConfig creates a new client with custom configuration
func NewRealHTTPClientWithConfig(httpClient *http.Client, baseURL string) *RealHTTPClient {
	defaultDataSource := types.LifecycleDataSourceUnknown
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}
	if baseURL == "" {
		baseURL = BaseURL
		defaultDataSource = types.LifecycleDataSourceEndOfLifeDate
	}
	return &RealHTTPClient{
		httpClient:        httpClient,
		baseURL:           baseURL,
		defaultDataSource: defaultDataSource,
	}
}

func lifecycleDataSource(value string, fallback types.LifecycleDataSource) types.LifecycleDataSource {
	switch types.LifecycleDataSource(strings.ToLower(strings.TrimSpace(value))) {
	case types.LifecycleDataSourceEndOfLifeDate:
		return types.LifecycleDataSourceEndOfLifeDate
	case types.LifecycleDataSourceLocalOverride:
		return types.LifecycleDataSourceLocalOverride
	default:
		return fallback
	}
}

// GetProductCycles retrieves all lifecycle cycles for a product from endoflife.date API
func (c *RealHTTPClient) GetProductCycles(ctx context.Context, product string) (ProductCyclesResult, error) {
	result := ProductCyclesResult{
		FetchedAt:  time.Now(),
		DataSource: c.defaultDataSource,
	}
	url := fmt.Sprintf("%s/%s.json", c.baseURL, product)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return result, errors.Wrap(err, "failed to create request")
	}

	// Set user agent for attribution
	req.Header.Set("User-Agent", "version-guard/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return result, errors.Wrapf(err, "failed to fetch data from %s", url)
	}
	defer resp.Body.Close()
	result.DataSource = lifecycleDataSource(resp.Header.Get(EOLSourceHeader), result.DataSource)

	if resp.StatusCode != http.StatusOK {
		// 404 is a meaningful signal: the product slug doesn't exist on
		// endoflife.date. Wrap with the typed sentinel so callers can
		// errors.Is(err, ErrProductNotFound) without sniffing the
		// message text.
		if resp.StatusCode == http.StatusNotFound {
			return result, errors.Wrapf(ErrProductNotFound, "product %q", product)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return result, errors.Errorf("unexpected status code %d (failed to read response body)", resp.StatusCode)
		}
		return result, errors.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(&result.Cycles); err != nil {
		return result, errors.Wrap(err, "failed to decode response")
	}

	return result, nil
}
