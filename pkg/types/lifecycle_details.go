package types

import "time"

// LifecycleDetails preserves structured lifecycle data on findings so
// downstream enrichment can reason about support windows without re-fetching EOL data.
type LifecycleDetails struct {
	StandardSupportEnd   *time.Time `json:"standard_support_end,omitempty"`
	DeprecatedSupportEnd *time.Time `json:"deprecated_support_end,omitempty"`
	EOLDate              *time.Time `json:"eol_date,omitempty"`
	ExtendedSupportEnd   *time.Time `json:"extended_support_end,omitempty"`
	ActionableDate       *time.Time `json:"actionable_date,omitempty"`
	ReleaseDate          *time.Time `json:"release_date,omitempty"`
	LatestReleaseDate    *time.Time `json:"latest_release_date,omitempty"`
	LTSDate              *time.Time `json:"lts_date,omitempty"`
	FetchedAt            time.Time  `json:"fetched_at,omitempty"`
	Version              string     `json:"version,omitempty"`
	Engine               string     `json:"engine,omitempty"`
	Source               string     `json:"source,omitempty"`
	IsSupported          bool       `json:"is_supported"`
	IsDeprecated         bool       `json:"is_deprecated"`
	IsExtendedSupport    bool       `json:"is_extended_support"`
	IsEOL                bool       `json:"is_eol"`
}

// LifecycleDetailsFromVersionLifecycle converts EOL provider output into
// finding-level lifecycle metadata.
func LifecycleDetailsFromVersionLifecycle(lifecycle *VersionLifecycle) LifecycleDetails {
	if lifecycle == nil {
		return LifecycleDetails{}
	}
	standardSupportEnd := lifecycle.DeprecationDate
	if standardSupportEnd == nil && lifecycle.ExtendedSupportEnd != nil {
		standardSupportEnd = lifecycle.EOLDate
	}
	actionableDate := firstLifecycleDate(
		standardSupportEnd,
		lifecycle.DeprecatedSupportEnd,
		lifecycle.ExtendedSupportEnd,
		lifecycle.EOLDate,
	)
	return LifecycleDetails{
		ReleaseDate:          lifecycle.ReleaseDate,
		LatestReleaseDate:    lifecycle.LatestReleaseDate,
		LTSDate:              lifecycle.LTSDate,
		StandardSupportEnd:   standardSupportEnd,
		DeprecatedSupportEnd: lifecycle.DeprecatedSupportEnd,
		EOLDate:              lifecycle.EOLDate,
		ExtendedSupportEnd:   lifecycle.ExtendedSupportEnd,
		ActionableDate:       actionableDate,
		FetchedAt:            lifecycle.FetchedAt,
		Version:              lifecycle.Version,
		Engine:               lifecycle.Engine,
		Source:               lifecycle.Source,
		IsSupported:          lifecycle.IsSupported,
		IsDeprecated:         lifecycle.IsDeprecated,
		IsExtendedSupport:    lifecycle.IsExtendedSupport,
		IsEOL:                lifecycle.IsEOL,
	}
}

func firstLifecycleDate(dates ...*time.Time) *time.Time {
	var first *time.Time
	for _, date := range dates {
		if date == nil {
			continue
		}
		if first == nil || date.Before(*first) {
			first = date
		}
	}
	return first
}
