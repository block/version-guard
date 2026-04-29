package policy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/block/Version-Guard/pkg/types"
)

// timePtr is a tiny helper so the tests below can stay readable.
func timePtr(t time.Time) *time.Time { return &t }

// ---------------- GetMessage / per-status message helpers ----------------

func TestGetMessage_Red_IsEOL(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "aurora-mysql", CurrentVersion: "5.6.10a"}
	eol := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)
	lc := &types.VersionLifecycle{IsEOL: true, EOLDate: &eol}

	got := p.GetMessage(res, lc, types.StatusRed)
	assert.Contains(t, got, "past End-of-Life")
	assert.Contains(t, got, "5.6.10a")
	assert.Contains(t, got, "Jun 2023")
}

func TestGetMessage_Red_IsDeprecated(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "postgres", CurrentVersion: "11.0"}
	dep := time.Date(2024, 5, 15, 0, 0, 0, 0, time.UTC)
	lc := &types.VersionLifecycle{IsDeprecated: true, DeprecationDate: &dep}

	got := p.GetMessage(res, lc, types.StatusRed)
	assert.Contains(t, got, "deprecated")
	assert.Contains(t, got, "May 2024")
}

func TestGetMessage_Red_ExtendedSupportEnded(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "aurora-postgresql", CurrentVersion: "11.21"}
	ended := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) // in the past
	lc := &types.VersionLifecycle{ExtendedSupportEnd: &ended}

	got := p.GetMessage(res, lc, types.StatusRed)
	assert.Contains(t, got, "Extended support")
	assert.Contains(t, got, "has ended")
}

func TestGetMessage_Red_Fallback(t *testing.T) {
	// Status forced to RED but none of the typed reasons apply — exercises
	// the bottom "requires immediate attention" fallback.
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "lambda", CurrentVersion: "nodejs14.x"}
	lc := &types.VersionLifecycle{}

	got := p.GetMessage(res, lc, types.StatusRed)
	assert.Contains(t, got, "requires immediate attention")
}

func TestGetMessage_Yellow_ExtendedSupport(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "aurora-mysql", CurrentVersion: "5.7"}
	lc := &types.VersionLifecycle{IsExtendedSupport: true}

	got := p.GetMessage(res, lc, types.StatusYellow)
	assert.Contains(t, got, "extended support")
	assert.Contains(t, got, "6x standard cost")
}

func TestGetMessage_Yellow_ApproachingEOL(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "redis", CurrentVersion: "6.0"}
	// Add a small extra buffer so the integer-truncated days-until
	// value is stable regardless of when the test runs (24h * 30 + 1h
	// guarantees we observe "30 days" not "29 days" after rounding).
	soon := time.Now().Add(30*24*time.Hour + time.Hour)
	lc := &types.VersionLifecycle{EOLDate: &soon}

	got := p.GetMessage(res, lc, types.StatusYellow)
	assert.Contains(t, got, "will reach End-of-Life")
	assert.Regexp(t, `\d+ days`, got)
}

func TestGetMessage_Yellow_ApproachingDeprecation(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "lambda", CurrentVersion: "nodejs18.x"}
	soon := time.Now().Add(45*24*time.Hour + time.Hour)
	lc := &types.VersionLifecycle{DeprecationDate: &soon}

	got := p.GetMessage(res, lc, types.StatusYellow)
	assert.Contains(t, got, "will be deprecated")
	assert.Regexp(t, `\d+ days`, got)
}

func TestGetMessage_Yellow_Fallback(t *testing.T) {
	// YELLOW with no typed reason — exercises the fallback branch
	// ("should be upgraded soon").
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "eks", CurrentVersion: "1.28"}
	lc := &types.VersionLifecycle{}

	got := p.GetMessage(res, lc, types.StatusYellow)
	assert.Contains(t, got, "should be upgraded soon")
}

func TestGetMessage_Green(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "postgres", CurrentVersion: "16.2"}
	lc := &types.VersionLifecycle{}

	got := p.GetMessage(res, lc, types.StatusGreen)
	assert.Contains(t, got, "currently supported")
}

func TestGetMessage_Unknown_NoLifecycle(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "lambda", CurrentVersion: "java8.al2"}
	lc := &types.VersionLifecycle{Version: ""} // no lifecycle data

	got := p.GetMessage(res, lc, types.StatusUnknown)
	assert.Contains(t, got, "No lifecycle data available")
}

func TestGetMessage_Unknown_HaveLifecycleButCantClassify(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "redis", CurrentVersion: "7.0"}
	lc := &types.VersionLifecycle{Version: "7.0"} // we have data, but not enough

	got := p.GetMessage(res, lc, types.StatusUnknown)
	assert.Contains(t, got, "Unable to determine support status")
}

func TestGetMessage_DefaultArm_BogusStatus(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "aurora-mysql", CurrentVersion: "8.0"}
	lc := &types.VersionLifecycle{}

	got := p.GetMessage(res, lc, types.Status("BOGUS"))
	assert.Contains(t, got, "Unknown status")
}

// ---------------- GetRecommendation ----------------

func TestGetRecommendation_Red_WithUpgradeTarget(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "aurora-mysql", CurrentVersion: "5.6"}
	lc := &types.VersionLifecycle{Version: "5.6", RecommendedVersion: "8.0"}

	got := p.GetRecommendation(res, lc, types.StatusRed)
	assert.Contains(t, got, "Upgrade to aurora-mysql 8.0")
	assert.Contains(t, got, "immediately")
}

func TestGetRecommendation_Red_NoUpgradeTarget(t *testing.T) {
	// RecommendedVersion empty -> generic wording.
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "redis", CurrentVersion: "5.0"}
	lc := &types.VersionLifecycle{Version: "5.0"}

	got := p.GetRecommendation(res, lc, types.StatusRed)
	assert.Contains(t, got, "Upgrade to the latest supported version")
}

func TestGetRecommendation_Yellow_ExtendedSupport_WithNonExtendedTarget(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "aurora-postgresql", CurrentVersion: "11"}
	lc := &types.VersionLifecycle{
		Version:                       "11",
		IsExtendedSupport:             true,
		RecommendedNonExtendedVersion: "16",
	}

	got := p.GetRecommendation(res, lc, types.StatusYellow)
	assert.Contains(t, got, "Upgrade to aurora-postgresql 16")
	assert.Contains(t, got, "avoid extended support costs")
}

func TestGetRecommendation_Yellow_ExtendedSupport_NoNonExtendedTarget(t *testing.T) {
	// Every supported cycle is itself in extended support — falls back
	// to the neutral wording rather than over-promising cost relief.
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "aurora-mysql", CurrentVersion: "5.6"}
	lc := &types.VersionLifecycle{Version: "5.6", IsExtendedSupport: true}

	got := p.GetRecommendation(res, lc, types.StatusYellow)
	assert.Contains(t, got, "Upgrade to a supported version")
	assert.Contains(t, got, "avoid extended support costs")
}

func TestGetRecommendation_Yellow_ApproachingEOL_WithTarget(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "redis", CurrentVersion: "6.0"}
	lc := &types.VersionLifecycle{Version: "6.0", RecommendedVersion: "7.2"}

	got := p.GetRecommendation(res, lc, types.StatusYellow)
	assert.Contains(t, got, "Plan upgrade to redis 7.2")
	assert.Contains(t, got, "within the next 90 days")
}

func TestGetRecommendation_Yellow_ApproachingEOL_NoTarget(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "redis", CurrentVersion: "6.0"}
	lc := &types.VersionLifecycle{Version: "6.0"}

	got := p.GetRecommendation(res, lc, types.StatusYellow)
	assert.Contains(t, got, "Plan upgrade to the latest supported version")
}

func TestGetRecommendation_Green(t *testing.T) {
	p := NewDefaultPolicy()
	got := p.GetRecommendation(&types.Resource{}, &types.VersionLifecycle{}, types.StatusGreen)
	assert.Equal(t, "No action required", got)
}

func TestGetRecommendation_Unknown(t *testing.T) {
	p := NewDefaultPolicy()
	got := p.GetRecommendation(&types.Resource{}, &types.VersionLifecycle{}, types.StatusUnknown)
	assert.Contains(t, got, "Verify version")
}

func TestGetRecommendation_DefaultArm_BogusStatus(t *testing.T) {
	p := NewDefaultPolicy()
	got := p.GetRecommendation(&types.Resource{}, &types.VersionLifecycle{}, types.Status("BOGUS"))
	assert.Equal(t, "Unable to provide recommendation", got)
}

// ---------------- usableUpgradeTarget edge cases ----------------

func TestUsableUpgradeTarget(t *testing.T) {
	res := &types.Resource{CurrentVersion: "5.6.10a"}
	lc := &types.VersionLifecycle{Version: "5.6"}

	// Empty candidate -> empty.
	assert.Empty(t, usableUpgradeTarget(res, lc, ""))

	// Candidate equals lifecycle cycle -> empty (would be a no-op).
	assert.Empty(t, usableUpgradeTarget(res, lc, "5.6"))

	// Candidate equals resource's current full version -> empty.
	assert.Empty(t, usableUpgradeTarget(res, lc, "5.6.10a"))

	// Different candidate -> returned.
	assert.Equal(t, "8.0", usableUpgradeTarget(res, lc, "8.0"))
}

// ---------------- versionMatches ----------------

func TestVersionMatches(t *testing.T) {
	tests := []struct {
		lifecycleVersion string
		resourceVersion  string
		want             bool
	}{
		// Exact match.
		{"5.6", "5.6", true},
		// Prefix match (resource has patch).
		{"5.6", "5.6.10a", true},
		// Mismatch.
		{"5.6", "5.7", false},
		// k8s- prefix stripped.
		{"1.31", "k8s-1.31.5", true},
		// kubernetes- prefix stripped.
		{"1.31", "kubernetes-1.31.5", true},
		// Empty resource.
		{"5.6", "", false},
		// Bare-major prefix should NOT match a different major.
		{"8", "8.0.35", true},  // "8.0.35" startsWith "8."
		{"8", "80.0.0", false}, // "80.0.0" does NOT startsWith "8." — guards against false-prefix
	}

	for _, tt := range tests {
		t.Run(tt.lifecycleVersion+"/"+tt.resourceVersion, func(t *testing.T) {
			assert.Equal(t, tt.want, versionMatches(tt.lifecycleVersion, tt.resourceVersion))
		})
	}
}

// ---------------- Classify branch coverage gaps ----------------

func TestClassify_GreenIsExplicitOnIsSupported(t *testing.T) {
	// Lifecycle has version match + IsSupported=true and no warning
	// flags -> GREEN. (Existing tests cover the EOL/yellow paths but
	// not this branch in default_test.go.)
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "postgres", CurrentVersion: "16.2"}
	lc := &types.VersionLifecycle{Version: "16.2", IsSupported: true}

	assert.Equal(t, types.StatusGreen, p.Classify(res, lc))
}

func TestClassify_FallsThroughToUnknownWhenNotSupportedAndNoSignal(t *testing.T) {
	// Version matches but lifecycle has IsSupported=false and no RED
	// or YELLOW signal — neither GREEN nor RED nor YELLOW applies, so
	// the function returns UNKNOWN.
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "redis", CurrentVersion: "7.0"}
	lc := &types.VersionLifecycle{Version: "7.0", IsSupported: false}

	assert.Equal(t, types.StatusUnknown, p.Classify(res, lc))
}

// keep timePtr referenced even if other tests stop using it
var _ = timePtr
