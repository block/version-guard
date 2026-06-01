package policy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/block/Version-Guard/pkg/types"
)

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
}

func TestGetMessage_Yellow_ExtendedSupport_WithEOL(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "eks", CurrentVersion: "1.32"}
	eol := time.Date(2027, 3, 23, 0, 0, 0, 0, time.UTC)
	lc := &types.VersionLifecycle{IsExtendedSupport: true, EOLDate: &eol}

	got := p.GetMessage(res, lc, types.StatusYellow)
	assert.Contains(t, got, "extended support")
	assert.NotContains(t, got, "End-of-Life")
}

func TestGetMessage_Yellow_DeprecatedSupport_WithEOL(t *testing.T) {
	// Lambda-style: IsDeprecatedSupport with a deprecated-support
	// end date. Exercises the IsDeprecatedSupport+EOLDate branch and
	// versionSubject's "Runtime" label for Lambda resources.
	p := NewDefaultPolicy()
	res := &types.Resource{
		Engine:         "aws-lambda",
		CurrentVersion: "nodejs16.x",
		Type:           types.ResourceType("lambda"),
	}
	end := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	lc := &types.VersionLifecycle{IsDeprecatedSupport: true, EOLDate: &end}

	got := p.GetMessage(res, lc, types.StatusYellow)
	assert.Contains(t, got, "Runtime nodejs16.x")
	assert.Contains(t, got, "deprecated support")
	assert.Contains(t, got, "Jun 12, 2026")
}

func TestGetMessage_Yellow_DeprecatedSupport_NoEOL(t *testing.T) {
	// IsDeprecatedSupport with no EOL date — exercises the
	// "deprecated support" branch that omits the until-date phrase.
	// Non-lambda resource so versionSubject returns "Version".
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "postgres", CurrentVersion: "11"}
	lc := &types.VersionLifecycle{IsDeprecatedSupport: true}

	got := p.GetMessage(res, lc, types.StatusYellow)
	assert.Contains(t, got, "Version 11")
	assert.Contains(t, got, "deprecated support")
	assert.NotContains(t, got, "until")
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
	assert.Contains(t, got, "will leave standard support")
	assert.Regexp(t, `\d+ days`, got)
}

func TestGetMessage_Yellow_UsesFirstLifecycleDate(t *testing.T) {
	p := NewDefaultPolicy()
	res := &types.Resource{Engine: "mysql", CurrentVersion: "8.0.41"}
	standardEnd := time.Now().Add(30*24*time.Hour + time.Hour)
	terminalEOL := time.Now().Add(3 * 365 * 24 * time.Hour)
	lc := &types.VersionLifecycle{DeprecationDate: &standardEnd, EOLDate: &terminalEOL}

	got := p.GetMessage(res, lc, types.StatusYellow)
	assert.Contains(t, got, "will leave standard support")
	assert.Contains(t, got, standardEnd.Format("Jan 2, 2006"))
	assert.NotContains(t, got, terminalEOL.Format("Jan 2, 2006"))
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
