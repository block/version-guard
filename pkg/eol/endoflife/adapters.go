package endoflife

import (
	"time"

	"github.com/pkg/errors"

	"github.com/block/Version-Guard/pkg/types"
)

// SchemaAdapter adapts endoflife.date ProductCycle to VersionLifecycle.
// Some products use non-standard field semantics and need custom
// adapters; see ADAPTERS.md for the catalog.
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
//  3. support + eol, no extendedSupport — pure OSS pattern (PostgreSQL etc.).
//     standardEnd = support, trueEOL = eol, no extended-support window.
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
	standardEnd *time.Time // last day of standard (free) support
	extendedEnd *time.Time // last day of extended (paid) support, if any
	trueEOL     *time.Time // last day the version is supported at all
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

// EKSSchemaAdapter handles amazon-eks, whose cycles ship in a shape
// that disagrees with the standard schema in two ways:
//
//   - There is no `support` field; `cycle.eol` is the end of *standard*
//     support (start of paid extended support), not a true terminal date.
//   - `cycle.extendedSupport` is now a *date* (the end of paid extended
//     support); it used to be a boolean and the adapter still tolerates
//     that legacy shape.
//   - EKS clusters never truly stop working, so we always leave
//     lifecycle.EOLDate = nil. Past-extended-support is still classified
//     RED via `IsDeprecated && !IsExtendedSupport`, matching the prior
//     product behavior of urging upgrades.
type EKSSchemaAdapter struct{}

// parseEKSExtendedEnd resolves cycle.extendedSupport into an end date.
// The current schema uses a date string; older cycles used boolean
// `true` to mean "extended support exists, ends at cycle.eol", so we
// fall back to standardEnd in that case to keep upgrade nudges firing.
func (a *EKSSchemaAdapter) parseEKSExtendedEnd(extSupport interface{}, standardEnd *time.Time) *time.Time {
	switch v := extSupport.(type) {
	case string:
		if v != "" && v != falseBool {
			if parsed, err := parseDate(v); err == nil {
				return &parsed
			}
		}
	case bool:
		if v && standardEnd != nil {
			return standardEnd
		}
	}
	return nil
}

// classifyEKS sets the lifecycle status flags from the derived
// boundaries. EKS has no true EOL, so past-extended-support is
// represented as RED via IsDeprecated && !IsExtendedSupport.
func (a *EKSSchemaAdapter) classifyEKS(lifecycle *types.VersionLifecycle, standardEnd, extendedEnd *time.Time) {
	now := time.Now()
	switch {
	case extendedEnd != nil && now.After(*extendedEnd):
		// Past extended support — AWS no longer issues patches.
		lifecycle.IsSupported = false
		lifecycle.IsDeprecated = true
		lifecycle.IsExtendedSupport = false
	case standardEnd != nil && now.After(*standardEnd):
		// In the paid extended-support window.
		lifecycle.IsSupported = true
		lifecycle.IsExtendedSupport = true
		lifecycle.IsDeprecated = true
	default:
		// Still in standard support (or no date info at all).
		lifecycle.IsSupported = true
	}
}

// AdaptCycle converts an amazon-eks ProductCycle to VersionLifecycle.
func (a *EKSSchemaAdapter) AdaptCycle(cycle *ProductCycle) (*types.VersionLifecycle, error) {
	lifecycle := &types.VersionLifecycle{
		Version:   cycle.Cycle,
		Engine:    "eks",
		Source:    providerName,
		FetchedAt: time.Now(),
	}

	if cycle.ReleaseDate != "" {
		if releaseDate, err := parseDate(cycle.ReleaseDate); err == nil {
			lifecycle.ReleaseDate = &releaseDate
		}
	}

	// cycle.eol is end-of-standard-support for amazon-eks.
	var standardEnd *time.Time
	if dateStr := anyToDateString(cycle.EOL); dateStr != "" {
		if parsed, err := parseDate(dateStr); err == nil {
			standardEnd = &parsed
		}
	}
	lifecycle.DeprecationDate = standardEnd
	lifecycle.ExtendedSupportEnd = a.parseEKSExtendedEnd(cycle.ExtendedSupport, standardEnd)
	// EKS has no true EOL — clusters keep running indefinitely.
	lifecycle.EOLDate = nil

	a.classifyEKS(lifecycle, standardEnd, lifecycle.ExtendedSupportEnd)

	return lifecycle, nil
}

// SchemaAdapters is a registry of available schema adapters.
var SchemaAdapters = map[string]SchemaAdapter{
	"standard":    &StandardSchemaAdapter{},
	"eks_adapter": &EKSSchemaAdapter{},
}

// GetSchemaAdapter returns the appropriate schema adapter for a product.
func GetSchemaAdapter(schemaName string) (SchemaAdapter, error) {
	adapter, ok := SchemaAdapters[schemaName]
	if !ok {
		return nil, errors.Errorf("unknown schema adapter: %s", schemaName)
	}
	return adapter, nil
}
