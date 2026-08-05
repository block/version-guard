package wiz

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
)

const (
	wizAuthURL    = "https://auth.app.wiz.io/oauth/token"
	wizGraphQLURL = "https://api.us13.app.wiz.io/graphql"

	maxRetries   = 5
	retryBackoff = 3 * time.Second

	reportFreshnessGrace = 6 * time.Hour
	maxFutureClockSkew   = 5 * time.Minute
)

const reportDownloadQuery = `query ReportDownloadUrl($reportId: ID!) {
  report(id: $reportId) {
    id
    name
	  runIntervalHours
    lastRun {
      status
      url
	  runAt
	  results {
	    __typename
	    ... on ReportRunResultsGraphQuery { rowCount: resultCount }
	    ... on ReportRunResultsCloudResource { rowCount: count }
	    ... on ReportRunResultsCloudResourceV2 { rowCount: count }
	  }
    }
  }
}`

// HTTPClient implements WizClient using net/http
type HTTPClient struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client

	// authURL and graphqlURL default to the Wiz production endpoints
	// in NewHTTPClient. They are package-private so tests can stand
	// up an httptest.Server and point the client at it without an
	// extra public constructor.
	authURL    string
	graphqlURL string
}

// NewHTTPClient creates a new HTTPClient for the Wiz API
func NewHTTPClient(clientID, clientSecret string) *HTTPClient {
	return &HTTPClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		authURL:      wizAuthURL,
		graphqlURL:   wizGraphQLURL,
	}
}

type accessTokenResponse struct {
	AccessToken string `json:"access_token"`
}

//nolint:govet // JSON field order chosen for readability of wire payload
type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type reportRunResponse struct {
	RunAt   time.Time `json:"runAt"`
	Results *struct {
		RowCount *int   `json:"rowCount"`
		Type     string `json:"__typename"`
	} `json:"results"`
	Status string `json:"status"`
	URL    string `json:"url"`
}

type reportResponse struct {
	Report *struct {
		LastRun          *reportRunResponse `json:"lastRun"`
		ID               string             `json:"id"`
		Name             string             `json:"name"`
		RunIntervalHours int                `json:"runIntervalHours"`
	} `json:"report"`
}

// GetAccessToken retrieves an OAuth2 access token from the Wiz auth endpoint
func (c *HTTPClient) GetAccessToken(ctx context.Context) (string, error) {
	params := url.Values{}
	params.Set("grant_type", "client_credentials")
	params.Set("audience", "beyond-api")
	params.Set("client_id", c.clientID)
	params.Set("client_secret", c.clientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.authURL, strings.NewReader(params.Encode()))
	if err != nil {
		return "", errors.Wrap(err, "failed to create auth request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Encoding", "UTF-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "failed to perform auth request")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errors.Wrap(err, "failed to read auth response body")
	}

	if resp.StatusCode != http.StatusOK {
		return "", errors.Errorf("Wiz auth failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp accessTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", errors.Wrap(err, "failed to parse auth response")
	}

	return tokenResp.AccessToken, nil
}

// GetReport retrieves report metadata including the download URL via GraphQL
func (c *HTTPClient) GetReport(ctx context.Context, accessToken, reportID string) (*Report, error) {
	gqlReq := graphQLRequest{
		Query: reportDownloadQuery,
		Variables: map[string]any{
			"reportId": reportID,
		},
	}

	reqBody, err := json.Marshal(gqlReq)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal GraphQL request")
	}

	var result reportResponse
	if err := c.doGraphQL(ctx, accessToken, reqBody, &result); err != nil {
		return nil, errors.Wrapf(err, "failed to get report %s", reportID)
	}

	return validateReportMetadata(result.Report, reportID, time.Now().UTC())
}

func validateReportMetadata(report *struct {
	LastRun          *reportRunResponse `json:"lastRun"`
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	RunIntervalHours int                `json:"runIntervalHours"`
}, reportID string, now time.Time) (*Report, error) {
	if report == nil {
		return nil, errors.Errorf("report %s not found", reportID)
	}
	if report.ID != reportID {
		return nil, errors.Errorf("report identity mismatch: requested %s, received %s", reportID, report.ID)
	}
	if strings.TrimSpace(report.Name) == "" {
		return nil, errors.Errorf("report %s has no name", reportID)
	}
	if report.LastRun == nil {
		return nil, errors.Errorf("report %s has no last run", reportID)
	}
	if report.LastRun.Status != "COMPLETED" {
		return nil, errors.Errorf("report %s run status is %s", reportID, report.LastRun.Status)
	}
	if strings.TrimSpace(report.LastRun.URL) == "" {
		return nil, errors.Errorf("report %s has no download URL", reportID)
	}
	if report.LastRun.RunAt.IsZero() {
		return nil, errors.Errorf("report %s has no run time", reportID)
	}
	if report.RunIntervalHours <= 0 {
		return nil, errors.Errorf("report %s has invalid run interval %d hours", reportID, report.RunIntervalHours)
	}
	if report.LastRun.Results == nil {
		return nil, errors.Errorf("report %s has no run results", reportID)
	}
	resultType := report.LastRun.Results.Type
	switch resultType {
	case "ReportRunResultsGraphQuery", "ReportRunResultsCloudResource", "ReportRunResultsCloudResourceV2":
	default:
		return nil, errors.Errorf("report %s has unsupported result type %q", reportID, resultType)
	}
	if report.LastRun.Results.RowCount == nil {
		return nil, errors.Errorf("report %s has no row count", reportID)
	}
	if *report.LastRun.Results.RowCount < 0 {
		return nil, errors.Errorf("report %s has invalid row count %d", reportID, *report.LastRun.Results.RowCount)
	}

	if report.LastRun.RunAt.After(now.Add(maxFutureClockSkew)) {
		return nil, errors.Errorf("report %s run time %s is in the future beyond allowed clock skew %s", reportID, report.LastRun.RunAt, maxFutureClockSkew)
	}
	maxAge := time.Duration(report.RunIntervalHours)*time.Hour + reportFreshnessGrace
	age := now.Sub(report.LastRun.RunAt)
	if age > maxAge {
		return nil, errors.Errorf("report %s run is stale: age %s exceeds maximum %s", reportID, age, maxAge)
	}

	return &Report{
		ID:               report.ID,
		Name:             report.Name,
		DownloadURL:      report.LastRun.URL,
		LastRun:          report.LastRun.RunAt,
		RunIntervalHours: report.RunIntervalHours,
		ExpectedRows:     *report.LastRun.Results.RowCount,
	}, nil
}

// DownloadReport downloads the report CSV from the provided URL
func (c *HTTPClient) DownloadReport(ctx context.Context, downloadURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, http.NoBody)
	if err != nil {
		return nil, errors.New("failed to create download request")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.New("failed to download report")
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, errors.Errorf("report download failed with status %d", resp.StatusCode)
	}

	return resp.Body, nil
}

// doGraphQL executes a GraphQL request with retry logic for rate limits
func (c *HTTPClient) doGraphQL(ctx context.Context, accessToken string, reqBody []byte, result any) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.graphqlURL, bytes.NewReader(reqBody))
		if err != nil {
			return errors.Wrap(err, "failed to create GraphQL request")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return errors.Wrap(err, "failed to perform GraphQL request")
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return errors.Wrap(err, "failed to read GraphQL response body")
		}

		if resp.StatusCode != http.StatusOK {
			errMsg := string(body)
			if strings.Contains(errMsg, "Rate limit exceeded") {
				lastErr = errors.Errorf("rate limit exceeded (attempt %d/%d)", i+1, maxRetries)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(retryBackoff):
					continue
				}
			}
			return errors.Errorf("GraphQL request failed with status %d: %s", resp.StatusCode, errMsg)
		}

		var gqlResp graphQLResponse
		if err := json.Unmarshal(body, &gqlResp); err != nil {
			return errors.Wrap(err, "failed to parse GraphQL response")
		}

		if len(gqlResp.Errors) > 0 {
			errMsg := gqlResp.Errors[0].Message
			if strings.Contains(errMsg, "Rate limit exceeded") {
				lastErr = errors.Errorf("rate limit exceeded (attempt %d/%d)", i+1, maxRetries)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(retryBackoff):
					continue
				}
			}
			return errors.Errorf("GraphQL error: %s", errMsg)
		}

		if err := json.Unmarshal(gqlResp.Data, result); err != nil {
			return errors.Wrap(err, "failed to unmarshal GraphQL data")
		}

		return nil
	}

	return errors.Wrap(lastErr, "all retries failed for GraphQL request")
}
