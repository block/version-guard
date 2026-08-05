package policy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/block/Version-Guard/pkg/types"
)

func TestUnknownCause(t *testing.T) {
	tests := []struct {
		name      string
		resource  *types.Resource
		lifecycle *types.VersionLifecycle
		status    types.Status
		want      types.LifecycleUnknownCause
	}{
		{
			name:      "provider cause wins",
			resource:  &types.Resource{CurrentVersion: "8.0.35"},
			lifecycle: &types.VersionLifecycle{UnknownCause: types.LifecycleUnknownCauseProductNotFound},
			status:    types.StatusUnknown,
			want:      types.LifecycleUnknownCauseProductNotFound,
		},
		{
			name:      "empty inventory version",
			resource:  &types.Resource{CurrentVersion: " "},
			lifecycle: &types.VersionLifecycle{},
			status:    types.StatusUnknown,
			want:      types.LifecycleUnknownCauseEmptyInventoryVersion,
		},
		{
			name:      "cycle absent",
			resource:  &types.Resource{CurrentVersion: "8.0.35"},
			lifecycle: &types.VersionLifecycle{},
			status:    types.StatusUnknown,
			want:      types.LifecycleUnknownCauseCycleNotFound,
		},
		{
			name:      "lifecycle mismatch",
			resource:  &types.Resource{CurrentVersion: "8.0.35"},
			lifecycle: &types.VersionLifecycle{Version: "5.7"},
			status:    types.StatusUnknown,
			want:      types.LifecycleUnknownCauseLifecycleMismatch,
		},
		{
			name:      "indeterminate lifecycle",
			resource:  &types.Resource{CurrentVersion: "8.0.35"},
			lifecycle: &types.VersionLifecycle{Version: "8.0"},
			status:    types.StatusUnknown,
			want:      types.LifecycleUnknownCauseIndeterminate,
		},
		{
			name:      "known status has no cause",
			resource:  &types.Resource{CurrentVersion: "8.0.35"},
			lifecycle: &types.VersionLifecycle{Version: "8.0", IsSupported: true},
			status:    types.StatusGreen,
		},
		{
			name:     "nil lifecycle",
			resource: &types.Resource{CurrentVersion: "8.0.35"},
			status:   types.StatusUnknown,
			want:     types.LifecycleUnknownCauseUnattributed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, UnknownCause(tt.resource, tt.lifecycle, tt.status))
		})
	}
}

func TestDefaultPolicy_Classify_EOLVersion(t *testing.T) {
	policy := NewDefaultPolicy()

	resource := &types.Resource{
		Engine:         "aurora-mysql",
		CurrentVersion: "5.6.10a",
	}

	eolDate := time.Now().AddDate(0, -6, 0) // 6 months ago
	lifecycle := &types.VersionLifecycle{
		Version:     "5.6.10a",
		Engine:      "aurora-mysql",
		IsEOL:       true,
		EOLDate:     &eolDate,
		IsSupported: false,
	}

	status := policy.Classify(resource, lifecycle)

	if status != types.StatusRed {
		t.Errorf("Expected RED status for EOL version, got %s", status)
	}
}

func TestDefaultPolicy_Classify_DeprecatedVersion(t *testing.T) {
	policy := NewDefaultPolicy()

	resource := &types.Resource{
		Engine:         "postgres",
		CurrentVersion: "11.0",
	}

	deprecationDate := time.Now().AddDate(0, -3, 0) // 3 months ago
	lifecycle := &types.VersionLifecycle{
		Version:         "11.0",
		Engine:          "postgres",
		IsDeprecated:    true,
		DeprecationDate: &deprecationDate,
		IsSupported:     false,
	}

	status := policy.Classify(resource, lifecycle)

	if status != types.StatusRed {
		t.Errorf("Expected RED status for deprecated version, got %s", status)
	}
}

func TestDefaultPolicy_Classify_ExtendedSupportExpired(t *testing.T) {
	policy := NewDefaultPolicy()

	resource := &types.Resource{
		Engine:         "mysql",
		CurrentVersion: "5.7.0",
	}

	extendedSupportEnd := time.Now().AddDate(0, -1, 0) // 1 month ago
	lifecycle := &types.VersionLifecycle{
		Version:            "5.7.0",
		Engine:             "mysql",
		IsExtendedSupport:  false,
		ExtendedSupportEnd: &extendedSupportEnd,
		IsSupported:        false,
	}

	status := policy.Classify(resource, lifecycle)

	if status != types.StatusRed {
		t.Errorf("Expected RED status for expired extended support, got %s", status)
	}
}

func TestDefaultPolicy_Classify_ExtendedSupport(t *testing.T) {
	policy := NewDefaultPolicy()
	policy.WarnExtendedSupport = true

	resource := &types.Resource{
		Engine:         "aurora-mysql",
		CurrentVersion: "5.7.12",
	}

	lifecycle := &types.VersionLifecycle{
		Version:           "5.7.12",
		Engine:            "aurora-mysql",
		IsExtendedSupport: true,
		IsSupported:       true,
	}

	status := policy.Classify(resource, lifecycle)

	if status != types.StatusYellow {
		t.Errorf("Expected YELLOW status for extended support, got %s", status)
	}
}

func TestDefaultPolicy_Classify_DeprecatedSupport(t *testing.T) {
	policy := NewDefaultPolicy()

	resource := &types.Resource{
		Engine:         "aws-lambda",
		CurrentVersion: "go1.x",
	}

	lifecycle := &types.VersionLifecycle{
		Version:             "go1.x",
		Engine:              "aws-lambda",
		IsDeprecated:        true,
		IsDeprecatedSupport: true,
		IsSupported:         true,
		DeprecationDate:     datePtr(time.Now().AddDate(0, -1, 0)),
		EOLDate:             datePtr(time.Now().AddDate(0, 5, 0)),
	}

	status := policy.Classify(resource, lifecycle)

	if status != types.StatusYellow {
		t.Errorf("Expected YELLOW status for deprecated support, got %s", status)
	}
}

func TestDefaultPolicy_Classify_ApproachingEOL(t *testing.T) {
	policy := NewDefaultPolicy()
	policy.EOLWarningDays = 90

	resource := &types.Resource{
		Engine:         "redis",
		CurrentVersion: "6.0",
	}

	eolDate := time.Now().AddDate(0, 0, 60) // 60 days from now
	lifecycle := &types.VersionLifecycle{
		Version:     "6.0",
		Engine:      "redis",
		EOLDate:     &eolDate,
		IsSupported: true,
	}

	status := policy.Classify(resource, lifecycle)

	if status != types.StatusYellow {
		t.Errorf("Expected YELLOW status for version approaching EOL (60 days), got %s", status)
	}
}

func TestDefaultPolicy_Classify_ApproachingDeprecation(t *testing.T) {
	policy := NewDefaultPolicy()
	policy.EOLWarningDays = 90

	resource := &types.Resource{
		Engine:         "opensearch",
		CurrentVersion: "1.3",
	}

	deprecationDate := time.Now().AddDate(0, 0, 45) // 45 days from now
	lifecycle := &types.VersionLifecycle{
		Version:         "1.3",
		Engine:          "opensearch",
		DeprecationDate: &deprecationDate,
		IsSupported:     true,
	}

	status := policy.Classify(resource, lifecycle)

	if status != types.StatusYellow {
		t.Errorf("Expected YELLOW status for version approaching deprecation (45 days), got %s", status)
	}
}

func TestDefaultPolicy_Classify_CurrentlySupported(t *testing.T) {
	policy := NewDefaultPolicy()

	resource := &types.Resource{
		Engine:         "aurora-mysql",
		CurrentVersion: "8.0.35",
	}

	eolDate := time.Now().AddDate(2, 0, 0) // 2 years from now
	lifecycle := &types.VersionLifecycle{
		Version:     "8.0.35",
		Engine:      "aurora-mysql",
		EOLDate:     &eolDate,
		IsSupported: true,
	}

	status := policy.Classify(resource, lifecycle)

	if status != types.StatusGreen {
		t.Errorf("Expected GREEN status for currently supported version, got %s", status)
	}
}

func TestDefaultPolicy_Classify_UnknownVersion(t *testing.T) {
	policy := NewDefaultPolicy()

	resource := &types.Resource{
		Engine:         "aurora-mysql",
		CurrentVersion: "9.9.9",
	}

	lifecycle := &types.VersionLifecycle{
		Version: "", // No lifecycle data
	}

	status := policy.Classify(resource, lifecycle)

	if status != types.StatusUnknown {
		t.Errorf("Expected UNKNOWN status for version with no lifecycle data, got %s", status)
	}
}

func TestDefaultPolicy_Classify_VersionMismatch(t *testing.T) {
	policy := NewDefaultPolicy()

	resource := &types.Resource{
		Engine:         "aurora-mysql",
		CurrentVersion: "8.0.35",
	}

	lifecycle := &types.VersionLifecycle{
		Version:     "8.0.28", // Different version
		Engine:      "aurora-mysql",
		IsSupported: true,
	}

	status := policy.Classify(resource, lifecycle)

	if status != types.StatusUnknown {
		t.Errorf("Expected UNKNOWN status for version mismatch, got %s", status)
	}
}

func TestDefaultPolicy_GetMessage_RedEOL(t *testing.T) {
	policy := NewDefaultPolicy()

	resource := &types.Resource{
		Engine:         "aurora-mysql",
		CurrentVersion: "5.6.10a",
	}

	eolDate := time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC)
	lifecycle := &types.VersionLifecycle{
		Version: "5.6.10a",
		Engine:  "aurora-mysql",
		IsEOL:   true,
		EOLDate: &eolDate,
	}

	message := policy.GetMessage(resource, lifecycle, types.StatusRed)

	expectedSubstring := "past End-of-Life"
	if !contains(message, expectedSubstring) {
		t.Errorf("Expected message to contain '%s', got: %s", expectedSubstring, message)
	}
}

func TestDefaultPolicy_GetMessage_YellowExtendedSupport(t *testing.T) {
	policy := NewDefaultPolicy()

	resource := &types.Resource{
		Engine:         "mysql",
		CurrentVersion: "5.7.0",
	}

	lifecycle := &types.VersionLifecycle{
		Version:           "5.7.0",
		Engine:            "mysql",
		IsExtendedSupport: true,
	}

	message := policy.GetMessage(resource, lifecycle, types.StatusYellow)

	expectedSubstring := "extended support"
	if !contains(message, expectedSubstring) {
		t.Errorf("Expected message to contain '%s', got: %s", expectedSubstring, message)
	}
	if contains(message, "6x") {
		t.Errorf("message must not hard-code extended support pricing: %s", message)
	}
}

func TestDefaultPolicy_GetMessage_YellowDeprecatedSupport(t *testing.T) {
	policy := NewDefaultPolicy()

	resource := &types.Resource{
		Engine:         "aws-lambda",
		CurrentVersion: "nodejs18.x",
	}

	lifecycle := &types.VersionLifecycle{
		Version:             "nodejs18.x",
		Engine:              "aws-lambda",
		IsDeprecated:        true,
		IsDeprecatedSupport: true,
		IsSupported:         true,
		EOLDate:             datePtr(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)),
	}

	message := policy.GetMessage(resource, lifecycle, types.StatusYellow)

	if !contains(message, "Runtime nodejs18.x for aws-lambda is in deprecated support until Sep 30, 2026") {
		t.Errorf("unexpected deprecated-support message: %s", message)
	}
	if contains(message, "extended support") || contains(message, "cost") {
		t.Errorf("deprecated-support message must not use extended-support cost wording: %s", message)
	}
}

func TestDefaultPolicy_GetMessage_Green(t *testing.T) {
	policy := NewDefaultPolicy()

	resource := &types.Resource{
		Engine:         "aurora-mysql",
		CurrentVersion: "8.0.35",
	}

	lifecycle := &types.VersionLifecycle{
		Version:     "8.0.35",
		Engine:      "aurora-mysql",
		IsSupported: true,
	}

	message := policy.GetMessage(resource, lifecycle, types.StatusGreen)

	expectedSubstring := "currently supported"
	if !contains(message, expectedSubstring) {
		t.Errorf("Expected message to contain '%s', got: %s", expectedSubstring, message)
	}
}

func TestDefaultPolicy_Name(t *testing.T) {
	policy := NewDefaultPolicy()

	name := policy.Name()

	expected := "DefaultVersionPolicy"
	if name != expected {
		t.Errorf("Expected policy name '%s', got: %s", expected, name)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func datePtr(t time.Time) *time.Time {
	return &t
}
