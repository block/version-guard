package endoflife

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProvider_GetVersionLifecycle_Product404 tests graceful degradation when
// a product doesn't exist on endoflife.date yet (e.g., aurora-mysql pending PR)
func TestProvider_GetVersionLifecycle_Product404(t *testing.T) {
	// Mock client that returns 404 error for product lookup
	mockClient := &MockClient{
		GetProductCyclesFunc: func(ctx context.Context, product string) ([]*ProductCycle, error) {
			// Simulate 404 from endoflife.date API
			return nil, &mockError{msg: "unexpected status code 404: Product not found"}
		},
	}

	provider := NewProvider(mockClient, 0, nil)

	// Try to get lifecycle for aurora-mysql (which returns 404)
	lifecycle, err := provider.GetVersionLifecycle(context.Background(), "aurora-mysql", "8.0.35")

	// Should NOT return error - graceful degradation
	require.NoError(t, err, "should gracefully handle 404 with UNKNOWN lifecycle, not error")

	// Should return UNKNOWN lifecycle
	assert.NotNil(t, lifecycle, "lifecycle should not be nil")
	assert.Equal(t, "", lifecycle.Version, "Version should be empty for UNKNOWN")
	assert.Equal(t, "aurora-mysql", lifecycle.Engine)
	assert.False(t, lifecycle.IsSupported, "IsSupported should be false for UNKNOWN")
}

// TestProvider_GetVersionLifecycle_Product404_ActualErrorMessage tests with
// realistic 404 error message from HTTP client
func TestProvider_GetVersionLifecycle_Product404_ActualErrorMessage(t *testing.T) {
	// Mock client with realistic 404 error message
	mockClient := &MockClient{
		GetProductCyclesFunc: func(ctx context.Context, product string) ([]*ProductCycle, error) {
			// Realistic error from RealHTTPClient
			return nil, &mockError{msg: "unexpected status code 404: Product not found"}
		},
	}

	provider := NewProvider(mockClient, 0, nil)

	// Try to get lifecycle for aurora-mysql
	lifecycle, err := provider.GetVersionLifecycle(context.Background(), "aurora-mysql", "8.0.35")

	// Should NOT return error - graceful degradation
	require.NoError(t, err, "should gracefully handle 404")

	// Should return UNKNOWN lifecycle
	assert.NotNil(t, lifecycle)
	assert.Equal(t, "", lifecycle.Version, "Version should be empty for UNKNOWN")
}

// TestProvider_ListAllVersions_Product404 tests that ListAllVersions returns
// empty list (not error) for 404
func TestProvider_ListAllVersions_Product404(t *testing.T) {
	mockClient := &MockClient{
		GetProductCyclesFunc: func(ctx context.Context, product string) ([]*ProductCycle, error) {
			return nil, &mockError{msg: "unexpected status code 404: Not Found"}
		},
	}

	provider := NewProvider(mockClient, 0, nil)

	// Try to list all versions for aurora-mysql
	versions, err := provider.ListAllVersions(context.Background(), "aurora-mysql")

	// Should return empty list, not error
	require.NoError(t, err, "should return empty list for 404, not error")
	assert.Empty(t, versions, "should return empty list when product not found")
}

// TestProvider_GetVersionLifecycle_NonProductErrors tests that real errors
// (not 404) still propagate correctly
func TestProvider_GetVersionLifecycle_NonProductErrors(t *testing.T) {
	tests := []struct {
		name        string
		errorMsg    string
		expectError bool
	}{
		{
			name:        "500 server error",
			errorMsg:    "unexpected status code 500: Internal Server Error",
			expectError: true,
		},
		{
			name:        "timeout error",
			errorMsg:    "context deadline exceeded",
			expectError: true,
		},
		{
			name:        "network error",
			errorMsg:    "connection refused",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockClient{
				GetProductCyclesFunc: func(ctx context.Context, product string) ([]*ProductCycle, error) {
					return nil, &mockError{msg: tt.errorMsg}
				},
			}

			provider := NewProvider(mockClient, 0, nil)

			_, err := provider.GetVersionLifecycle(context.Background(), "aurora-mysql", "8.0.35")

			if tt.expectError {
				assert.Error(t, err, "non-404 errors should propagate")
			} else {
				assert.NoError(t, err, "should not error")
			}
		})
	}
}

// mockError is a simple error type for testing
type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}
