package policy

import (
	"fmt"
	"strings"
	"time"

	"github.com/block/Version-Guard/pkg/types"
)

// DefaultPolicy implements the standard version compliance classification rules
type DefaultPolicy struct {
	// EOLWarningDays is the number of days before EOL to start showing YELLOW warnings
	EOLWarningDays int

	// WarnExtendedSupport indicates whether to warn about extended support versions
	WarnExtendedSupport bool
}

// NewDefaultPolicy creates a new DefaultPolicy with standard settings
func NewDefaultPolicy() *DefaultPolicy {
	return &DefaultPolicy{
		EOLWarningDays:      90,
		WarnExtendedSupport: true,
	}
}

// Name returns the name of this policy
func (p *DefaultPolicy) Name() string {
	return "DefaultVersionPolicy"
}

// Classify determines the compliance status based on version lifecycle
//
// Classification Rules:
// - RED: Past EOL, unsupported deprecated, or extended support expired
// - YELLOW: In extended/deprecated support, or approaching EOL (< 90 days)
// - GREEN: Current supported version
// - UNKNOWN: Version not found in EOL database
func (p *DefaultPolicy) Classify(resource *types.Resource, lifecycle *types.VersionLifecycle) types.Status {
	// If lifecycle data is empty or version doesn't match, return UNKNOWN.
	// endoflife.date uses major.minor cycles (e.g., "8.0") while resources
	// have full versions (e.g., "8.0.35"), so we use prefix matching.
	if lifecycle.Version == "" || !versionMatches(lifecycle.Version, resource.CurrentVersion) {
		return types.StatusUnknown
	}

	// Check for RED status conditions
	if p.isRedStatus(lifecycle) {
		return types.StatusRed
	}

	// Check for YELLOW status conditions
	if p.isYellowStatus(lifecycle) {
		return types.StatusYellow
	}

	// GREEN: Currently supported
	if lifecycle.IsSupported {
		return types.StatusGreen
	}

	// Default to UNKNOWN if we can't determine status
	return types.StatusUnknown
}

// isRedStatus checks if the lifecycle indicates a RED status
func (p *DefaultPolicy) isRedStatus(lifecycle *types.VersionLifecycle) bool {
	// Past End-of-Life
	if lifecycle.IsEOL {
		return true
	}

	// Deprecated (but not if still in a supported warning window)
	if lifecycle.IsDeprecated && !lifecycle.IsExtendedSupport && !lifecycle.IsDeprecatedSupport {
		return true
	}

	// Extended support has ended
	if lifecycle.ExtendedSupportEnd != nil && time.Now().After(*lifecycle.ExtendedSupportEnd) {
		return true
	}

	return false
}

// isYellowStatus checks if the lifecycle indicates a YELLOW status
func (p *DefaultPolicy) isYellowStatus(lifecycle *types.VersionLifecycle) bool {
	// In deprecated support (for example Lambda deprecated runtimes).
	if lifecycle.IsDeprecatedSupport {
		return true
	}

	// In extended support.
	if p.WarnExtendedSupport && lifecycle.IsExtendedSupport {
		return true
	}

	// Approaching the first actionable lifecycle boundary. Later support
	// dates are still exposed in the finding's eol block, but they should not
	// create additional yellow warning windows before the first date arrives.
	if warningDate, ok := firstFutureLifecycleDate(lifecycle); ok {
		daysUntilWarning := int(time.Until(*warningDate).Hours() / 24)
		if daysUntilWarning > 0 && daysUntilWarning <= p.EOLWarningDays {
			return true
		}
	}

	return false
}

// GetMessage generates a human-readable message describing the status
func (p *DefaultPolicy) GetMessage(resource *types.Resource, lifecycle *types.VersionLifecycle, status types.Status) string {
	switch status {
	case types.StatusRed:
		return p.getRedMessage(resource, lifecycle)
	case types.StatusYellow:
		return p.getYellowMessage(resource, lifecycle)
	case types.StatusGreen:
		return p.getGreenMessage(resource, lifecycle)
	case types.StatusUnknown:
		return p.getUnknownMessage(resource, lifecycle)
	default:
		return fmt.Sprintf("Unknown status for %s version %s", resource.Engine, resource.CurrentVersion)
	}
}

func (p *DefaultPolicy) getRedMessage(resource *types.Resource, lifecycle *types.VersionLifecycle) string {
	if lifecycle.IsEOL && lifecycle.EOLDate != nil {
		return fmt.Sprintf("Version %s of %s is past End-of-Life (EOL since %s)",
			resource.CurrentVersion,
			resource.Engine,
			lifecycle.EOLDate.Format("Jan 2006"))
	}

	if lifecycle.IsDeprecated && lifecycle.DeprecationDate != nil {
		return fmt.Sprintf("Version %s of %s is deprecated (since %s)",
			resource.CurrentVersion,
			resource.Engine,
			lifecycle.DeprecationDate.Format("Jan 2006"))
	}

	if lifecycle.ExtendedSupportEnd != nil && time.Now().After(*lifecycle.ExtendedSupportEnd) {
		return fmt.Sprintf("Extended support for %s version %s has ended (ended %s)",
			resource.Engine,
			resource.CurrentVersion,
			lifecycle.ExtendedSupportEnd.Format("Jan 2006"))
	}

	return fmt.Sprintf("Version %s of %s requires immediate attention", resource.CurrentVersion, resource.Engine)
}

func (p *DefaultPolicy) getYellowMessage(resource *types.Resource, lifecycle *types.VersionLifecycle) string {
	if lifecycle.IsDeprecatedSupport {
		end := lifecycle.DeprecatedSupportEnd
		if end == nil {
			end = lifecycle.EOLDate
		}
		if end != nil {
			return fmt.Sprintf("%s %s for %s is in deprecated support until %s",
				versionSubject(resource),
				resource.CurrentVersion,
				resource.Engine,
				end.Format("Jan 2, 2006"))
		}
		return fmt.Sprintf("%s %s for %s is in deprecated support",
			versionSubject(resource),
			resource.CurrentVersion,
			resource.Engine)
	}

	if lifecycle.IsExtendedSupport {
		return fmt.Sprintf("Version %s of %s is in extended support",
			resource.CurrentVersion,
			resource.Engine)
	}

	if warningDate, ok := firstFutureLifecycleDate(lifecycle); ok {
		daysUntilWarning := int(time.Until(*warningDate).Hours() / 24)
		if daysUntilWarning > 0 && daysUntilWarning <= p.EOLWarningDays {
			if lifecycle.DeprecationDate != nil && warningDate.Equal(*lifecycle.DeprecationDate) {
				return fmt.Sprintf("Version %s of %s will leave standard support in %d days (on %s)",
					resource.CurrentVersion,
					resource.Engine,
					daysUntilWarning,
					warningDate.Format("Jan 2, 2006"))
			}
			return fmt.Sprintf("Version %s of %s will reach End-of-Life in %d days (on %s)",
				resource.CurrentVersion,
				resource.Engine,
				daysUntilWarning,
				warningDate.Format("Jan 2, 2006"))
		}
	}

	return fmt.Sprintf("Version %s of %s should be upgraded soon", resource.CurrentVersion, resource.Engine)
}

//nolint:unparam // lifecycle may be used in future enhancements
func (p *DefaultPolicy) getGreenMessage(resource *types.Resource, lifecycle *types.VersionLifecycle) string {
	return fmt.Sprintf("Version %s of %s is currently supported", resource.CurrentVersion, resource.Engine)
}

func (p *DefaultPolicy) getUnknownMessage(resource *types.Resource, lifecycle *types.VersionLifecycle) string {
	if lifecycle.Version == "" {
		return fmt.Sprintf("No lifecycle data available for %s version %s", resource.Engine, resource.CurrentVersion)
	}

	return fmt.Sprintf("Unable to determine support status for %s version %s", resource.Engine, resource.CurrentVersion)
}

func versionSubject(resource *types.Resource) string {
	if isLambda(resource) {
		return "Runtime"
	}
	return "Version"
}

func isLambda(resource *types.Resource) bool {
	return resource.Engine == "aws-lambda" || strings.EqualFold(resource.Type.String(), "lambda")
}

func firstFutureLifecycleDate(lifecycle *types.VersionLifecycle) (*time.Time, bool) {
	var first *time.Time
	now := time.Now()
	for _, date := range []*time.Time{
		lifecycle.DeprecationDate,
		lifecycle.DeprecatedSupportEnd,
		lifecycle.ExtendedSupportEnd,
		lifecycle.EOLDate,
	} {
		if date == nil || !date.After(now) {
			continue
		}
		if first == nil || date.Before(*first) {
			first = date
		}
	}
	return first, first != nil
}

// versionMatches checks if a resource version matches a lifecycle version.
// endoflife.date uses major.minor cycles (e.g., "8.0") while resources may have
// full versions (e.g., "8.0.35") or prefixed versions (e.g., "k8s-1.33").
func versionMatches(lifecycleVersion, resourceVersion string) bool {
	if lifecycleVersion == resourceVersion {
		return true
	}
	// Strip common prefixes for comparison
	normalized := resourceVersion
	for _, prefix := range []string{"k8s-", "kubernetes-"} {
		normalized = strings.TrimPrefix(normalized, prefix)
	}
	if lifecycleVersion == normalized {
		return true
	}
	return strings.HasPrefix(normalized, lifecycleVersion+".")
}
