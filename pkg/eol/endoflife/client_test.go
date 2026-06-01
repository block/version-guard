package endoflife

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
			cycles, err := client.GetProductCycles(context.Background(), tt.product)

			// Verify
			if (err != nil) != tt.wantErr {
				t.Errorf("GetProductCycles() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(cycles) != tt.wantCycles {
				t.Errorf("GetProductCycles() got %d cycles, want %d", len(cycles), tt.wantCycles)
			}

			// Verify first cycle if successful
			if !tt.wantErr && tt.wantCycles > 0 {
				if cycles[0].Cycle != "1.31" {
					t.Errorf("First cycle = %s, want 1.31", cycles[0].Cycle)
				}
				if cycles[0].ReleaseDate != "2024-11-19" {
					t.Errorf("First cycle release date = %s, want 2024-11-19", cycles[0].ReleaseDate)
				}
				if cycles[0].LatestReleaseDate != "2025-01-15" {
					t.Errorf("First cycle latest release date = %s, want 2025-01-15", cycles[0].LatestReleaseDate)
				}
				if cycles[0].LTS != "2025-02-01" {
					t.Errorf("First cycle lts = %v, want 2025-02-01", cycles[0].LTS)
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
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Product not found"}`))
	}))
	defer server.Close()

	client := NewRealHTTPClientWithConfig(&http.Client{Timeout: 5 * time.Second}, server.URL)
	_, err := client.GetProductCycles(context.Background(), "non-existent")

	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !errors.Is(err, ErrProductNotFound) {
		t.Errorf("404 should wrap ErrProductNotFound, got %v", err)
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
