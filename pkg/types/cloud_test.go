package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCloudProvider_String(t *testing.T) {
	tests := []struct {
		provider CloudProvider
		want     string
	}{
		{CloudProviderAWS, "AWS"},
		{CloudProviderGCP, "GCP"},
		{CloudProviderAzure, "AZURE"},
		{CloudProviderUnknown, "UNKNOWN"},
		{CloudProvider("BOGUS"), "BOGUS"},
	}
	for _, tt := range tests {
		t.Run(string(tt.provider), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.provider.String())
		})
	}
}

// TestCloudProvider_IsValid locks in current behavior: CloudProviderUnknown
// is intentionally NOT considered "valid" — IsValid is meant to gate
// deserialization paths that should reject sentinel-or-bogus inputs, while
// `Unknown` is a sentinel returned by callers that observe the absence
// of a CloudProvider.
func TestCloudProvider_IsValid(t *testing.T) {
	tests := []struct {
		provider CloudProvider
		want     bool
	}{
		{CloudProviderAWS, true},
		{CloudProviderGCP, true},
		{CloudProviderAzure, true},
		{CloudProviderUnknown, false}, // sentinel, not "valid"
		{CloudProvider(""), false},
		{CloudProvider("BOGUS"), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.provider), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.provider.IsValid())
		})
	}
}
