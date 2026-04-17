package endoflife

import (
	"time"

	"github.com/pkg/errors"

	"github.com/block/Version-Guard/pkg/types"
)

// SchemaAdapter adapts endoflife.date ProductCycle to VersionLifecycle
// Some products use non-standard field semantics and need custom adapters
type SchemaAdapter interface {
	AdaptCycle(cycle *ProductCycle) (*types.VersionLifecycle, error)
}

// StandardSchemaAdapter handles products with standard endoflife.date schema
// Standard semantics:
//   - cycle.support → DeprecationDate (end of standard support)
//   - cycle.eol → EOLDate (true end of life)
//   - cycle.extendedSupport → ExtendedSupportEnd
type StandardSchemaAdapter struct{}

// lifecycleDates holds parsed date values for lifecycle calculations
type lifecycleDates struct {
	eol             *time.Time
	support         *time.Time
	extendedSupport *time.Time
}

// parseCycleDates extracts and parses dates from a ProductCycle
func (a *StandardSchemaAdapter) parseCycleDates(cycle *ProductCycle) lifecycleDates {
	dates := lifecycleDates{}

	// Parse EOL date (STANDARD semantics: true end of life)
	if dateStr := anyToDateString(cycle.EOL); dateStr != "" {
		if parsed, err := parseDate(dateStr); err == nil {
			dates.eol = &parsed
		}
	}

	// Parse support date (STANDARD semantics: end of standard support)
	if dateStr := anyToDateString(cycle.Support); dateStr != "" {
		if parsed, err := parseDate(dateStr); err == nil {
			dates.support = &parsed
		}
	}

	// Parse extended support
	if cycle.ExtendedSupport != nil {
		dates.extendedSupport = a.parseExtendedSupport(cycle.ExtendedSupport, dates.eol)
	}

	return dates
}

// parseExtendedSupport handles the extended support field which can be string or bool
func (a *StandardSchemaAdapter) parseExtendedSupport(extSupport interface{}, eolDate *time.Time) *time.Time {
	switch v := extSupport.(type) {
	case string:
		if v != "" && v != falseBool {
			if parsed, err := parseDate(v); err == nil {
				return &parsed
			}
		}
	case bool:
		// If boolean true, use EOL date as extended support end
		if v && eolDate != nil {
			return eolDate
		}
	}
	return nil
}

// setLifecycleStatus determines lifecycle status flags based on dates
func (a *StandardSchemaAdapter) setLifecycleStatus(lifecycle *types.VersionLifecycle, dates lifecycleDates) {
	now := time.Now()

	// If we have an EOL date and we're past it, mark as EOL
	if dates.eol != nil && now.After(*dates.eol) {
		lifecycle.IsEOL = true
		lifecycle.IsSupported = false
		lifecycle.IsDeprecated = true
		return
	}

	// If we have extended support end and we're past standard support
	if dates.extendedSupport != nil && dates.support != nil && now.After(*dates.support) {
		a.setExtendedSupportStatus(lifecycle, dates, now)
		return
	}

	// If we're past support date but no extended support info
	if dates.support != nil && now.After(*dates.support) {
		lifecycle.IsDeprecated = true
		lifecycle.IsSupported = false
		// If we have EOL date and not past it yet, not EOL
		if dates.eol != nil && now.Before(*dates.eol) {
			lifecycle.IsEOL = false
		}
		return
	}

	// Still in standard support
	lifecycle.IsSupported = true
	lifecycle.IsDeprecated = false
	lifecycle.IsEOL = false
}

// setExtendedSupportStatus handles status when in or past extended support window
func (a *StandardSchemaAdapter) setExtendedSupportStatus(lifecycle *types.VersionLifecycle, dates lifecycleDates, now time.Time) {
	if now.Before(*dates.extendedSupport) {
		// In extended support window
		lifecycle.IsSupported = true
		lifecycle.IsExtendedSupport = true
		lifecycle.IsDeprecated = true
	} else {
		// Past extended support
		lifecycle.IsEOL = true
		lifecycle.IsSupported = false
		lifecycle.IsDeprecated = true
	}
}

// AdaptCycle converts a ProductCycle to VersionLifecycle using standard semantics
func (a *StandardSchemaAdapter) AdaptCycle(cycle *ProductCycle) (*types.VersionLifecycle, error) {
	lifecycle := &types.VersionLifecycle{
		Version:   cycle.Cycle,
		Engine:    "", // Set by caller
		Source:    providerName,
		FetchedAt: time.Now(),
	}

	// Parse release date
	if cycle.ReleaseDate != "" {
		if releaseDate, err := parseDate(cycle.ReleaseDate); err == nil {
			lifecycle.ReleaseDate = &releaseDate
		}
	}

	// Parse lifecycle dates
	dates := a.parseCycleDates(cycle)

	// Set dates on lifecycle
	lifecycle.EOLDate = dates.eol
	lifecycle.DeprecationDate = dates.support
	lifecycle.ExtendedSupportEnd = dates.extendedSupport

	// Determine lifecycle status
	a.setLifecycleStatus(lifecycle, dates)

	return lifecycle, nil
}

// EKSSchemaAdapter handles amazon-eks product with NON-STANDARD schema
// EKS semantics (DIFFERENT from standard):
//   - cycle.support → DeprecationDate (end of standard support) ✅ Same
//   - cycle.eol → ExtendedSupportEnd (NOT true EOL!) ⚠️ DIFFERENT
//   - cycle.extendedSupport → boolean flag (NOT a date) ⚠️ DIFFERENT
//   - EKS has NO true EOL (clusters keep running forever)
type EKSSchemaAdapter struct{}

// AdaptCycle converts EKS ProductCycle to VersionLifecycle using EKS-specific semantics
func (a *EKSSchemaAdapter) AdaptCycle(cycle *ProductCycle) (*types.VersionLifecycle, error) {
	lifecycle := &types.VersionLifecycle{
		Version:   cycle.Cycle,
		Engine:    "eks",
		Source:    providerName,
		FetchedAt: time.Now(),
	}

	// Parse release date (standard)
	if cycle.ReleaseDate != "" {
		if releaseDate, err := parseDate(cycle.ReleaseDate); err == nil {
			lifecycle.ReleaseDate = &releaseDate
		}
	}

	// Parse standard support end (standard)
	var supportDate *time.Time
	if dateStr := anyToDateString(cycle.Support); dateStr != "" {
		if parsed, err := parseDate(dateStr); err == nil {
			supportDate = &parsed
			lifecycle.DeprecationDate = supportDate
		}
	}

	// ⚠️ NON-STANDARD: cycle.EOL → ExtendedSupportEnd (NOT EOLDate!)
	var extendedSupportEnd *time.Time
	if dateStr := anyToDateString(cycle.EOL); dateStr != "" {
		if parsed, err := parseDate(dateStr); err == nil {
			extendedSupportEnd = &parsed
			lifecycle.ExtendedSupportEnd = &parsed
		}
	}

	// EKS has NO true EOL (clusters keep running forever)
	lifecycle.EOLDate = nil

	// Determine lifecycle status
	now := time.Now()

	if extendedSupportEnd != nil && now.After(*extendedSupportEnd) {
		// Past extended support
		lifecycle.IsEOL = false // NOT true EOL, just no AWS support
		lifecycle.IsSupported = false
		lifecycle.IsDeprecated = true
		lifecycle.IsExtendedSupport = false
	} else if supportDate != nil && now.After(*supportDate) {
		// In extended support window
		lifecycle.IsSupported = true
		lifecycle.IsExtendedSupport = true
		lifecycle.IsDeprecated = true
		lifecycle.IsEOL = false
	} else {
		// Still in standard support
		lifecycle.IsSupported = true
		lifecycle.IsDeprecated = false
		lifecycle.IsEOL = false
		lifecycle.IsExtendedSupport = false
	}

	return lifecycle, nil
}

// SchemaAdapters is a registry of available schema adapters
var SchemaAdapters = map[string]SchemaAdapter{
	"standard":    &StandardSchemaAdapter{},
	"eks_adapter": &EKSSchemaAdapter{},
}

// GetSchemaAdapter returns the appropriate schema adapter for a product
func GetSchemaAdapter(schemaName string) (SchemaAdapter, error) {
	adapter, ok := SchemaAdapters[schemaName]
	if !ok {
		return nil, errors.Errorf("unknown schema adapter: %s", schemaName)
	}
	return adapter, nil
}
