package endoflife

import (
	"time"

	"github.com/pkg/errors"

	"github.com/block/Version-Guard/pkg/types"
)

// SchemaAdapter adapts endoflife.date ProductCycle to VersionLifecycle.
// Most products use the built-in standard schema; product-specific
// field semantics should prefer DeclarativeSchemaAdapter so adding a
// new product is a YAML change rather than a Go adapter.
type SchemaAdapter interface {
	AdaptCycle(cycle *ProductCycle) (*types.VersionLifecycle, error)
}

// StandardSchemaAdapter handles products with standard endoflife.date semantics.
//
// The adapter recognizes three field shapes that real upstream cycles
// take, and unifies them into the canonical VersionLifecycle dates:
//
//  1. support + eol + extendedSupport (date) — Aurora pattern.
//     standardEnd = support, extendedEnd = extendedSupport, trueEOL = extendedSupport.
//
//  2. eol + extendedSupport (date), no support — AWS ElastiCache pattern.
//     `eol` here is upstream shorthand for "end of standard support" because
//     `extendedSupport` is the real terminal date. standardEnd = eol,
//     extendedEnd = extendedSupport, trueEOL = extendedSupport.
//
//  3. support + eol, no extendedSupport — deprecated-support pattern
//     (AWS Lambda runtimes, and OSS products with maintenance/security
//     support after active support). standardEnd = support,
//     deprecatedSupportEnd = trueEOL = eol, no paid extended-support window.
//
// Legacy boolean `extendedSupport: true` is honored: when paired with
// a date `eol`, the adapter treats `eol` itself as the end of the
// extended-support window. `false` boolean is treated as no extended
// support.
type StandardSchemaAdapter struct{}

// lifecycleDates holds the raw parsed dates from a cycle, before
// semantic interpretation in setLifecycleStatus.
type lifecycleDates struct {
	eol             *time.Time
	support         *time.Time
	extendedSupport *time.Time // only set if cycle.extendedSupport was a *date string*
	extendedTrue    bool       // legacy: cycle.extendedSupport was bool true
}

func (a *StandardSchemaAdapter) parseCycleDates(cycle *ProductCycle) lifecycleDates {
	dates := lifecycleDates{}

	if dateStr := anyToDateString(cycle.EOL); dateStr != "" {
		if parsed, err := parseDate(dateStr); err == nil {
			dates.eol = &parsed
		}
	}

	if dateStr := anyToDateString(cycle.Support); dateStr != "" {
		if parsed, err := parseDate(dateStr); err == nil {
			dates.support = &parsed
		}
	}

	switch v := cycle.ExtendedSupport.(type) {
	case string:
		if v != "" && v != falseBool {
			if parsed, err := parseDate(v); err == nil {
				dates.extendedSupport = &parsed
			}
		}
	case bool:
		if v {
			dates.extendedTrue = true
		}
	}

	return dates
}

// derivedBoundaries collapses the raw parsed dates into the three
// semantic boundaries the policy layer cares about.
type derivedBoundaries struct {
	standardEnd          *time.Time // last day of standard support
	deprecatedSupportEnd *time.Time // last day of deprecated/non-paid support, if any
	extendedEnd          *time.Time // last day of extended support, if any
	trueEOL              *time.Time // last day the version is supported at all
}

func (a *StandardSchemaAdapter) deriveBoundaries(dates lifecycleDates) derivedBoundaries {
	b := derivedBoundaries{}

	switch {
	case dates.extendedSupport != nil:
		// Extended support window with an explicit end date (Aurora,
		// ElastiCache, or any product that ships an extendedSupport
		// date alongside eol/support).
		b.extendedEnd = dates.extendedSupport
		b.trueEOL = dates.extendedSupport
		switch {
		case dates.support != nil:
			b.standardEnd = dates.support
		case dates.eol != nil:
			// AWS pattern: no `support` field — `eol` is end of standard support
			// because `extendedSupport` is the real terminal date.
			b.standardEnd = dates.eol
		}
	case dates.extendedTrue && dates.eol != nil:
		// Legacy boolean: treat eol as the extended-support end.
		b.extendedEnd = dates.eol
		b.trueEOL = dates.eol
		if dates.support != nil {
			b.standardEnd = dates.support
		}
	case dates.support != nil && dates.eol != nil && dates.eol.After(*dates.support):
		// No extendedSupport field: the API is describing a warning
		// window after active/standard support but before terminal EOL.
		// This is not paid extended support, so policy must not use
		// cost-avoidance wording.
		b.standardEnd = dates.support
		b.deprecatedSupportEnd = dates.eol
		b.trueEOL = dates.eol
	default:
		// No extended support concept — the standard pattern.
		b.trueEOL = dates.eol
		if dates.support != nil {
			b.standardEnd = dates.support
		}
	}

	return b
}

func (a *StandardSchemaAdapter) classify(lifecycle *types.VersionLifecycle, b derivedBoundaries) {
	now := time.Now()

	switch {
	case b.trueEOL != nil && now.After(*b.trueEOL):
		// Past true end of life — no support of any kind remains.
		lifecycle.IsEOL = true
		lifecycle.IsSupported = false
		lifecycle.IsDeprecated = true
	case b.extendedEnd != nil && b.standardEnd != nil &&
		now.After(*b.standardEnd) && now.Before(*b.extendedEnd):
		// In the paid extended-support window.
		lifecycle.IsSupported = true
		lifecycle.IsExtendedSupport = true
		lifecycle.IsDeprecated = true
	case b.deprecatedSupportEnd != nil && b.standardEnd != nil &&
		now.After(*b.standardEnd) && now.Before(*b.deprecatedSupportEnd):
		// In a deprecated-support window. This remains YELLOW-worthy,
		// but it is not the paid extended-support state.
		lifecycle.IsSupported = true
		lifecycle.IsDeprecated = true
		lifecycle.IsDeprecatedSupport = true
	case b.standardEnd != nil && now.After(*b.standardEnd):
		// Past standard support but no extended support is available
		// (or we're past it without a true-EOL date pinning RED).
		lifecycle.IsDeprecated = true
		lifecycle.IsSupported = false
	default:
		// Still in standard support, or no date info at all.
		lifecycle.IsSupported = true
	}
}

// AdaptCycle converts a ProductCycle to VersionLifecycle using the
// standard semantics described in the StandardSchemaAdapter doc.
func (a *StandardSchemaAdapter) AdaptCycle(cycle *ProductCycle) (*types.VersionLifecycle, error) {
	lifecycle := &types.VersionLifecycle{
		Version:   cycle.Cycle,
		Engine:    "", // Set by caller
		Source:    providerName,
		FetchedAt: time.Now(),
	}

	if cycle.ReleaseDate != "" {
		if releaseDate, err := parseDate(cycle.ReleaseDate); err == nil {
			lifecycle.ReleaseDate = &releaseDate
		}
	}

	dates := a.parseCycleDates(cycle)
	b := a.deriveBoundaries(dates)

	lifecycle.DeprecationDate = b.standardEnd
	lifecycle.ExtendedSupportEnd = b.extendedEnd
	lifecycle.EOLDate = b.trueEOL

	a.classify(lifecycle, b)

	return lifecycle, nil
}

const (
	SchemaStandard    = "standard"
	SchemaDeclarative = "declarative"

	lifecycleFieldEOL             = "eol"
	lifecycleFieldSupport         = "support"
	lifecycleFieldExtendedSupport = "extendedSupport"

	lifecycleActionExtendedSupport = "extended_support"
	lifecycleActionUnsupported     = "unsupported"
	lifecycleActionEOL             = "eol"
	lifecycleActionSupported       = "supported"
)

// DeclarativeLifecycleConfig lets YAML describe product-specific
// lifecycle semantics without adding another Go adapter. It maps
// endoflife.date fields into VersionLifecycle boundaries, then declares
// how to classify the post-standard-support windows.
//
// Supported field names: support, eol, extendedSupport.
// Supported actions: extended_support, unsupported, eol, supported.
type DeclarativeLifecycleConfig struct {
	DeprecationDate    LifecycleDateSource `yaml:"deprecation_date"`
	ExtendedSupportEnd LifecycleDateSource `yaml:"extended_support_end"`
	EOLDate            LifecycleDateSource `yaml:"eol_date"`

	// DeprecatedWindow is applied after DeprecationDate and before
	// ExtendedSupportEnd. Most AWS paid/deprecated support windows use
	// "extended_support", which policy reports as YELLOW.
	DeprecatedWindow string `yaml:"deprecated_window"`

	// PastExtendedSupport is applied after ExtendedSupportEnd when
	// EOLDate is omitted or later than ExtendedSupportEnd. EKS uses
	// "unsupported" here because clusters keep running, but AWS stops
	// patching them.
	PastExtendedSupport string `yaml:"past_extended_support"`
}

// LifecycleDateSource declares which ProductCycle field supplies a
// VersionLifecycle date. BoolTrueFallback handles archived upstream
// shapes such as old EKS data where extendedSupport was true instead of
// a date.
type LifecycleDateSource struct {
	Field            string `yaml:"field"`
	BoolTrueFallback string `yaml:"bool_true_fallback,omitempty"`
}

// DeclarativeSchemaAdapter adapts cycles according to a YAML-provided
// DeclarativeLifecycleConfig.
type DeclarativeSchemaAdapter struct {
	config *DeclarativeLifecycleConfig
}

// NewDeclarativeSchemaAdapter validates config and returns an adapter.
func NewDeclarativeSchemaAdapter(config *DeclarativeLifecycleConfig) (*DeclarativeSchemaAdapter, error) {
	if err := ValidateDeclarativeLifecycleConfig(config); err != nil {
		return nil, err
	}
	return &DeclarativeSchemaAdapter{config: config}, nil
}

// ValidateDeclarativeLifecycleConfig checks that a YAML lifecycle block
// only references fields and actions the generic adapter understands.
func ValidateDeclarativeLifecycleConfig(config *DeclarativeLifecycleConfig) error {
	if config == nil {
		return errors.New("declarative lifecycle config is required")
	}

	for name, source := range map[string]LifecycleDateSource{
		"deprecation_date":     config.DeprecationDate,
		"extended_support_end": config.ExtendedSupportEnd,
		"eol_date":             config.EOLDate,
	} {
		if err := validateLifecycleDateSource(source); err != nil {
			return errors.Wrapf(err, "%s", name)
		}
	}

	if err := validateLifecycleAction(config.DeprecatedWindow, true); err != nil {
		return errors.Wrap(err, "deprecated_window")
	}
	if err := validateLifecycleAction(config.PastExtendedSupport, true); err != nil {
		return errors.Wrap(err, "past_extended_support")
	}

	if config.DeprecationDate.Field == "" &&
		config.ExtendedSupportEnd.Field == "" &&
		config.EOLDate.Field == "" {
		return errors.New("at least one lifecycle date source is required")
	}

	return nil
}

func validateLifecycleDateSource(source LifecycleDateSource) error {
	if source.Field != "" {
		if !isSupportedLifecycleField(source.Field) {
			return errors.Errorf("unsupported field %q", source.Field)
		}
	}
	if source.BoolTrueFallback != "" {
		if source.Field == "" {
			return errors.New("bool_true_fallback requires field")
		}
		if !isSupportedLifecycleField(source.BoolTrueFallback) {
			return errors.Errorf("unsupported bool_true_fallback field %q", source.BoolTrueFallback)
		}
	}
	return nil
}

func isSupportedLifecycleField(field string) bool {
	switch field {
	case lifecycleFieldEOL, lifecycleFieldSupport, lifecycleFieldExtendedSupport:
		return true
	default:
		return false
	}
}

func validateLifecycleAction(action string, allowEmpty bool) error {
	if action == "" && allowEmpty {
		return nil
	}
	switch action {
	case lifecycleActionExtendedSupport, lifecycleActionUnsupported, lifecycleActionEOL, lifecycleActionSupported:
		return nil
	default:
		return errors.Errorf("unsupported action %q", action)
	}
}

// AdaptCycle converts a ProductCycle to VersionLifecycle using the
// declarative YAML semantics.
func (a *DeclarativeSchemaAdapter) AdaptCycle(cycle *ProductCycle) (*types.VersionLifecycle, error) {
	lifecycle := &types.VersionLifecycle{
		Version:   cycle.Cycle,
		Engine:    "", // Set by caller
		Source:    providerName,
		FetchedAt: time.Now(),
	}

	if cycle.ReleaseDate != "" {
		if releaseDate, err := parseDate(cycle.ReleaseDate); err == nil {
			lifecycle.ReleaseDate = &releaseDate
		}
	}

	lifecycle.DeprecationDate = a.parseDateSource(cycle, a.config.DeprecationDate)
	lifecycle.ExtendedSupportEnd = a.parseDateSource(cycle, a.config.ExtendedSupportEnd)
	lifecycle.EOLDate = a.parseDateSource(cycle, a.config.EOLDate)

	a.classify(lifecycle)

	return lifecycle, nil
}

func (a *DeclarativeSchemaAdapter) parseDateSource(cycle *ProductCycle, source LifecycleDateSource) *time.Time {
	value, ok := cycleFieldValue(cycle, source.Field)
	if !ok {
		return nil
	}

	if dateStr := anyToDateString(value); dateStr != "" {
		if parsed, err := parseDate(dateStr); err == nil {
			return &parsed
		}
	}

	if boolValue, ok := value.(bool); ok && boolValue && source.BoolTrueFallback != "" {
		fallback, ok := cycleFieldValue(cycle, source.BoolTrueFallback)
		if !ok {
			return nil
		}
		if dateStr := anyToDateString(fallback); dateStr != "" {
			if parsed, err := parseDate(dateStr); err == nil {
				return &parsed
			}
		}
	}

	return nil
}

func cycleFieldValue(cycle *ProductCycle, field string) (any, bool) {
	switch field {
	case lifecycleFieldEOL:
		return cycle.EOL, true
	case lifecycleFieldSupport:
		return cycle.Support, true
	case lifecycleFieldExtendedSupport:
		return cycle.ExtendedSupport, true
	default:
		return nil, false
	}
}

func (a *DeclarativeSchemaAdapter) classify(lifecycle *types.VersionLifecycle) {
	now := time.Now()

	switch {
	case lifecycle.EOLDate != nil && now.After(*lifecycle.EOLDate):
		applyLifecycleAction(lifecycle, lifecycleActionEOL)
	case lifecycle.ExtendedSupportEnd != nil && now.After(*lifecycle.ExtendedSupportEnd):
		applyLifecycleAction(lifecycle, defaultLifecycleAction(a.config.PastExtendedSupport, lifecycleActionUnsupported))
	case lifecycle.DeprecationDate != nil && lifecycle.ExtendedSupportEnd != nil &&
		now.After(*lifecycle.DeprecationDate) && now.Before(*lifecycle.ExtendedSupportEnd):
		applyLifecycleAction(lifecycle, defaultLifecycleAction(a.config.DeprecatedWindow, lifecycleActionUnsupported))
	case lifecycle.DeprecationDate != nil && now.After(*lifecycle.DeprecationDate):
		applyLifecycleAction(lifecycle, lifecycleActionUnsupported)
	default:
		applyLifecycleAction(lifecycle, lifecycleActionSupported)
	}
}

func defaultLifecycleAction(action, fallback string) string {
	if action == "" {
		return fallback
	}
	return action
}

func applyLifecycleAction(lifecycle *types.VersionLifecycle, action string) {
	switch action {
	case lifecycleActionExtendedSupport:
		lifecycle.IsSupported = true
		lifecycle.IsDeprecated = true
		lifecycle.IsExtendedSupport = true
	case lifecycleActionUnsupported:
		lifecycle.IsSupported = false
		lifecycle.IsDeprecated = true
		lifecycle.IsExtendedSupport = false
	case lifecycleActionEOL:
		lifecycle.IsEOL = true
		lifecycle.IsSupported = false
		lifecycle.IsDeprecated = true
		lifecycle.IsExtendedSupport = false
	case lifecycleActionSupported:
		lifecycle.IsSupported = true
		lifecycle.IsDeprecated = false
		lifecycle.IsExtendedSupport = false
	}
}

// SchemaAdapters is a registry of available schema adapters.
var SchemaAdapters = map[string]SchemaAdapter{
	SchemaStandard: &StandardSchemaAdapter{},
}

// GetSchemaAdapter returns the appropriate schema adapter for a product.
func GetSchemaAdapter(schemaName string) (SchemaAdapter, error) {
	adapter, ok := SchemaAdapters[schemaName]
	if !ok {
		return nil, errors.Errorf("unknown schema adapter: %s", schemaName)
	}
	return adapter, nil
}
