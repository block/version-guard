package wiz

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestHTTPClient builds an HTTPClient pointed at the supplied test
// servers. Used by every http_client test below — keeps each test
// from repeating the constructor + URL plumbing.
func newTestHTTPClient(authURL, graphqlURL string) *HTTPClient {
	return &HTTPClient{
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
		httpClient:   &http.Client{Timeout: 5 * time.Second},
		authURL:      authURL,
		graphqlURL:   graphqlURL,
	}
}

func TestNewHTTPClient_DefaultsToProductionURLs(t *testing.T) {
	c := NewHTTPClient("id", "secret")
	require.NotNil(t, c)
	assert.Equal(t, "id", c.clientID)
	assert.Equal(t, "secret", c.clientSecret)
	assert.Equal(t, wizAuthURL, c.authURL, "auth URL must default to the Wiz production endpoint")
	assert.Equal(t, wizGraphQLURL, c.graphqlURL, "graphql URL must default to the Wiz production endpoint")
	assert.NotNil(t, c.httpClient)
	assert.Equal(t, 30*time.Second, c.httpClient.Timeout)
}

// ---------------- GetAccessToken ----------------

func TestGetAccessToken_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Auth endpoint receives form-encoded credentials.
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

		require.NoError(t, r.ParseForm())
		assert.Equal(t, "client_credentials", r.Form.Get("grant_type"))
		assert.Equal(t, "beyond-api", r.Form.Get("audience"))
		assert.Equal(t, "test-client-id", r.Form.Get("client_id"))
		assert.Equal(t, "test-client-secret", r.Form.Get("client_secret"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"opaque-token-xyz"}`))
	}))
	defer srv.Close()

	c := newTestHTTPClient(srv.URL, "")
	tok, err := c.GetAccessToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "opaque-token-xyz", tok)
}

func TestGetAccessToken_Non200Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid_client", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newTestHTTPClient(srv.URL, "")
	_, err := c.GetAccessToken(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth failed")
	assert.Contains(t, err.Error(), "401")
}

func TestGetAccessToken_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not-json`))
	}))
	defer srv.Close()

	c := newTestHTTPClient(srv.URL, "")
	_, err := c.GetAccessToken(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestGetAccessToken_TransportError(t *testing.T) {
	// Closed server -> connection refused. Exercises the failed-Do branch.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	c := newTestHTTPClient(srv.URL, "")
	_, err := c.GetAccessToken(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth request")
}

// ---------------- GetReport ----------------

func TestGetReport_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Auth header is forwarded.
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"report": {
					"id": "rep-1",
					"name": "Aurora Inventory",
					"lastRun": {"status": "COMPLETED", "url": "https://files.example/abc.csv"}
				}
			}
		}`))
	}))
	defer srv.Close()

	c := newTestHTTPClient("", srv.URL)
	rep, err := c.GetReport(context.Background(), "test-token", "rep-1")
	require.NoError(t, err)
	assert.Equal(t, "rep-1", rep.ID)
	assert.Equal(t, "Aurora Inventory", rep.Name)
	assert.Equal(t, "https://files.example/abc.csv", rep.DownloadURL)
}

func TestGetReport_LastRunNotCompleted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"data": {"report": {"id":"r","name":"n","lastRun":{"status":"FAILED","url":""}}}
		}`))
	}))
	defer srv.Close()

	c := newTestHTTPClient("", srv.URL)
	_, err := c.GetReport(context.Background(), "tok", "r")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FAILED")
}

func TestGetReport_GraphQLErrorArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors": [{"message":"unauthorized"}]}`))
	}))
	defer srv.Close()

	c := newTestHTTPClient("", srv.URL)
	_, err := c.GetReport(context.Background(), "tok", "r")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unauthorized")
}

func TestGetReport_HTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestHTTPClient("", srv.URL)
	_, err := c.GetReport(context.Background(), "tok", "r")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

// ---------------- doGraphQL retry behavior ----------------

func TestDoGraphQL_RateLimitRetriesThenSucceeds(t *testing.T) {
	// First call returns a 429-style "Rate limit exceeded" body, second
	// returns a happy COMPLETED report. Verifies the retry loop honors
	// the rate-limit substring detection AND that a per-attempt success
	// breaks out of the loop.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "Rate limit exceeded — slow down", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{
			"data":{"report":{"id":"r","name":"n","lastRun":{"status":"COMPLETED","url":"u"}}}
		}`))
	}))
	defer srv.Close()

	// Override retryBackoff via a shorter context-aware path: replace the
	// package-level constant by stubbing the HTTPClient's timeout to be
	// far longer than the backoff. retryBackoff is 3s; the test will
	// take ~3s but that's acceptable for a single test.
	c := newTestHTTPClient("", srv.URL)
	c.httpClient.Timeout = 30 * time.Second

	rep, err := c.GetReport(context.Background(), "tok", "r")
	require.NoError(t, err)
	assert.Equal(t, "r", rep.ID)
	assert.Equal(t, 2, calls, "client must have retried after the rate-limit hit")
}

func TestDoGraphQL_ContextCancelDuringBackoff(t *testing.T) {
	// Server always returns rate-limit; we cancel the context during
	// the backoff sleep and expect the GraphQL caller to return the
	// context error rather than continuing to retry.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newTestHTTPClient("", srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := c.GetReport(ctx, "tok", "r")
	require.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "context canceled") || strings.Contains(err.Error(), "context deadline"),
		"expected context-cancellation error, got: %s", err.Error())
}

// ---------------- DownloadReport ----------------

func TestDownloadReport_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte("col1,col2\nv1,v2\n"))
	}))
	defer srv.Close()

	c := newTestHTTPClient("", "")
	rc, err := c.DownloadReport(context.Background(), srv.URL)
	require.NoError(t, err)
	defer rc.Close()

	body, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Contains(t, string(body), "col1,col2")
}

func TestDownloadReport_Non200Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "expired", http.StatusForbidden)
	}))
	defer srv.Close()

	c := newTestHTTPClient("", "")
	_, err := c.DownloadReport(context.Background(), srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestDownloadReport_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close() // close immediately so connect refuses

	c := newTestHTTPClient("", "")
	_, err := c.DownloadReport(context.Background(), srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "download")
}
