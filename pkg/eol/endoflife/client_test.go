package endoflife

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/block/Version-Guard/pkg/types"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRealHTTPClient_GetProductCycles(t *testing.T) {
	tests := []struct {
		name           string
		product        string
		responseBody   string
		responseStatus int
		wantErr        bool
		wantCycles     int
	}{
		{
			name:           "successful request for amazon-eks",
			product:        "amazon-eks",
			responseStatus: http.StatusOK,
			responseBody: `[
				{
					"cycle": "1.31",
					"releaseDate": "2024-11-19",
					"latestReleaseDate": "2025-01-15",
					"support": "2025-12-19",
					"eol": "2027-05-19",
					"extendedSupport": true,
					"lts": "2025-02-01"
				},
				{
					"cycle": "1.30",
					"releaseDate": "2024-05-29",
					"support": "2025-06-29",
					"eol": "2026-11-29",
					"extendedSupport": true,
					"lts": false
				}
			]`,
			wantErr:    false,
			wantCycles: 2,
		},
		{
			name:           "404 not found",
			product:        "non-existent-product",
			responseStatus: http.StatusNotFound,
			responseBody:   `{"error": "Product not found"}`,
			wantErr:        true,
		},
		{
			name:           "500 server error",
			product:        "amazon-eks",
			responseStatus: http.StatusInternalServerError,
			responseBody:   `Internal Server Error`,
			wantErr:        true,
		},
		{
			name:           "invalid JSON response",
			product:        "amazon-eks",
			responseStatus: http.StatusOK,
			responseBody:   `{invalid json}`,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.responseStatus)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			// Create client with test server URL
			client := NewRealHTTPClientWithConfig(
				&http.Client{Timeout: 5 * time.Second},
				server.URL,
			)

			// Execute
			result, err := client.GetProductCycles(context.Background(), tt.product)

			// Verify
			if (err != nil) != tt.wantErr {
				t.Errorf("GetProductCycles() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(result.Cycles) != tt.wantCycles {
				t.Errorf("GetProductCycles() got %d cycles, want %d", len(result.Cycles), tt.wantCycles)
			}

			// Verify first cycle if successful
			if !tt.wantErr && tt.wantCycles > 0 {
				if result.Cycles[0].Cycle != "1.31" {
					t.Errorf("First cycle = %s, want 1.31", result.Cycles[0].Cycle)
				}
				if result.Cycles[0].ReleaseDate != "2024-11-19" {
					t.Errorf("First cycle release date = %s, want 2024-11-19", result.Cycles[0].ReleaseDate)
				}
				if result.Cycles[0].LatestReleaseDate != "2025-01-15" {
					t.Errorf("First cycle latest release date = %s, want 2025-01-15", result.Cycles[0].LatestReleaseDate)
				}
				if result.Cycles[0].LTS != "2025-02-01" {
					t.Errorf("First cycle lts = %v, want 2025-02-01", result.Cycles[0].LTS)
				}
			}
		})
	}
}

// TestRealHTTPClient_404ReturnsTypedError pins the contract that a 404 from
// endoflife.date is wrapped in ErrProductNotFound — callers (the Provider
// 404 cache path in particular) must be able to discriminate via errors.Is
// without sniffing the message text.
func TestRealHTTPClient_404ReturnsTypedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(EOLSourceHeader, string(types.LifecycleDataSourceLocalOverride))
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Product not found"}`))
	}))
	defer server.Close()

	client := NewRealHTTPClientWithConfig(&http.Client{Timeout: 5 * time.Second}, server.URL)
	result, err := client.GetProductCycles(context.Background(), "non-existent")

	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !errors.Is(err, ErrProductNotFound) {
		t.Errorf("404 should wrap ErrProductNotFound, got %v", err)
	}
	if result.DataSource != types.LifecycleDataSourceLocalOverride {
		t.Errorf("DataSource = %q, want %q", result.DataSource, types.LifecycleDataSourceLocalOverride)
	}
	if result.FetchedAt.IsZero() {
		t.Error("FetchedAt should be non-zero for 404 response")
	}
}

func TestRealHTTPClient_ProductCyclesResultSource(t *testing.T) {
	tests := []struct {
		name       string
		baseURL    func(string) string
		header     string
		wantSource types.LifecycleDataSource
	}{
		{
			name:       "custom endpoint with local override header",
			baseURL:    func(serverURL string) string { return serverURL },
			header:     "local_override",
			wantSource: types.LifecycleDataSourceLocalOverride,
		},
		{
			name:       "custom endpoint without header",
			baseURL:    func(serverURL string) string { return serverURL },
			wantSource: types.LifecycleDataSourceUnknown,
		},
		{
			name:       "invalid source header",
			baseURL:    func(serverURL string) string { return serverURL },
			header:     "attacker-controlled-value",
			wantSource: types.LifecycleDataSourceUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.header != "" {
					w.Header().Set(EOLSourceHeader, tt.header)
				}
				_, _ = w.Write([]byte(`[]`))
			}))
			defer server.Close()

			client := NewRealHTTPClientWithConfig(nil, tt.baseURL(server.URL))
			result, err := client.GetProductCycles(context.Background(), "test")
			if err != nil {
				t.Fatalf("GetProductCycles() error = %v", err)
			}
			if result.DataSource != tt.wantSource {
				t.Errorf("DataSource = %q, want %q", result.DataSource, tt.wantSource)
			}
			if result.FetchedAt.IsZero() {
				t.Error("FetchedAt should be non-zero")
			}
		})
	}
}

func TestNewRealHTTPClient_DefaultDataSource(t *testing.T) {
	client := NewRealHTTPClient()
	client.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = "example.test"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`[]`)),
			Request:    req,
		}, nil
	})

	result, err := client.GetProductCycles(context.Background(), "test")
	if err != nil {
		t.Fatalf("GetProductCycles() error = %v", err)
	}
	if result.DataSource != types.LifecycleDataSourceEndOfLifeDate {
		t.Errorf("DataSource = %q, want %q", result.DataSource, types.LifecycleDataSourceEndOfLifeDate)
	}
	if result.FetchedAt.IsZero() {
		t.Error("FetchedAt should be non-zero")
	}
}

func TestRealHTTPClient_UserAgent(t *testing.T) {
	var receivedUserAgent string

	// Create test server that captures User-Agent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUserAgent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	// Create client
	client := NewRealHTTPClientWithConfig(nil, server.URL)
	_, _ = client.GetProductCycles(context.Background(), "test")

	// Verify User-Agent is set
	if receivedUserAgent != "version-guard/1.0" {
		t.Errorf("User-Agent = %s, want version-guard/1.0", receivedUserAgent)
	}
}

func TestRealHTTPClient_ContextCancellation(t *testing.T) {
	// Create a slow server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	// Create client
	client := NewRealHTTPClientWithConfig(nil, server.URL)

	// Create context that cancels immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Execute - should fail due to canceled context
	_, err := client.GetProductCycles(ctx, "test")
	if err == nil {
		t.Error("Expected error due to canceled context, got nil")
	}
}
