package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatus_String(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusRed, "RED"},
		{StatusYellow, "YELLOW"},
		{StatusGreen, "GREEN"},
		{StatusUnknown, "UNKNOWN"},
		{Status("BOGUS"), "BOGUS"}, // String just casts; no validation
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.String())
		})
	}
}

func TestStatus_IsValid(t *testing.T) {
	tests := []struct {
		status Status
		want   bool
	}{
		{StatusRed, true},
		{StatusYellow, true},
		{StatusGreen, true},
		{StatusUnknown, true},
		{Status(""), false},
		{Status("BOGUS"), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.IsValid())
		})
	}
}

// TestStatus_Severity locks in the ordering used by the policy and
// summary layers (RED > YELLOW > GREEN > UNKNOWN). Reordering would
// silently flip "most severe finding" semantics in any caller that
// sorts by severity.
func TestStatus_Severity(t *testing.T) {
	assert.Greater(t, StatusRed.Severity(), StatusYellow.Severity())
	assert.Greater(t, StatusYellow.Severity(), StatusGreen.Severity())
	assert.Greater(t, StatusGreen.Severity(), StatusUnknown.Severity())

	// Concrete values, also locked in:
	assert.Equal(t, 3, StatusRed.Severity())
	assert.Equal(t, 2, StatusYellow.Severity())
	assert.Equal(t, 1, StatusGreen.Severity())
	assert.Equal(t, 0, StatusUnknown.Severity())

	// Unknown literal returns -1
	assert.Equal(t, -1, Status("BOGUS").Severity())
}
