package endoflife

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStandardSchemaAdapter_CurrentVersion(t *testing.T) {
	adapter := &StandardSchemaAdapter{}

	// Future dates to ensure version is current
	futureYear := time.Now().Year() + 2
	cycle := &ProductCycle{
		Cycle:       "16.1",
		ReleaseDate: "2024-01-15",
		Support:     time.Date(futureYear, 1, 15, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),
		EOL:         time.Date(futureYear+2, 1, 15, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)
	require.NotNil(t, lifecycle)

	assert.Equal(t, "16.1", lifecycle.Version)
	assert.Equal(t, providerName, lifecycle.Source)
	assert.True(t, lifecycle.IsSupported)
	assert.False(t, lifecycle.IsDeprecated)
	assert.False(t, lifecycle.IsEOL)
	assert.False(t, lifecycle.IsExtendedSupport)

	// Verify dates
	assert.NotNil(t, lifecycle.ReleaseDate)
	assert.NotNil(t, lifecycle.DeprecationDate)
	assert.NotNil(t, lifecycle.EOLDate)
}

func TestStandardSchemaAdapter_DeprecatedSupportWindow(t *testing.T) {
	adapter := &StandardSchemaAdapter{}

	// Version past standard support but before EOL, with no
	// extendedSupport field. This is deprecated support, not paid
	// extended support.
	cycle := &ProductCycle{
		Cycle:       "15.0",
		ReleaseDate: "2023-01-15",
		Support:     "2024-01-15", // Past
		EOL:         "2028-01-15", // Future
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.True(t, lifecycle.IsSupported)
	assert.True(t, lifecycle.IsDeprecated)
	assert.True(t, lifecycle.IsDeprecatedSupport)
	assert.False(t, lifecycle.IsExtendedSupport)
	assert.False(t, lifecycle.IsEOL)
}

func TestStandardSchemaAdapter_EOLVersion(t *testing.T) {
	adapter := &StandardSchemaAdapter{}

	// Version past EOL
	cycle := &ProductCycle{
		Cycle:       "14.0",
		ReleaseDate: "2022-01-15",
		Support:     "2023-01-15", // Past
		EOL:         "2024-01-15", // Past
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.False(t, lifecycle.IsSupported)
	assert.True(t, lifecycle.IsDeprecated)
	assert.True(t, lifecycle.IsEOL)
}

func TestStandardSchemaAdapter_ExtendedSupport(t *testing.T) {
	adapter := &StandardSchemaAdapter{}

	// Version in extended support window
	pastYear := time.Now().Year() - 1
	futureYear := time.Now().Year() + 2
	cycle := &ProductCycle{
		Cycle:           "13.0",
		ReleaseDate:     "2021-01-15",
		Support:         time.Date(pastYear, 1, 15, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),     // Past
		EOL:             time.Date(futureYear+2, 1, 15, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), // Future
		ExtendedSupport: time.Date(futureYear, 1, 15, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),   // Future (as string)
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.True(t, lifecycle.IsSupported)
	assert.True(t, lifecycle.IsDeprecated)
	assert.False(t, lifecycle.IsEOL)
	assert.True(t, lifecycle.IsExtendedSupport)
	assert.NotNil(t, lifecycle.ExtendedSupportEnd)
}

func TestStandardSchemaAdapter_ExtendedSupportBoolean(t *testing.T) {
	adapter := &StandardSchemaAdapter{}

	// ExtendedSupport as boolean true
	pastYear := time.Now().Year() - 1
	futureYear := time.Now().Year() + 2
	cycle := &ProductCycle{
		Cycle:           "13.0",
		ReleaseDate:     "2021-01-15",
		Support:         time.Date(pastYear, 1, 15, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),   // Past
		EOL:             time.Date(futureYear, 1, 15, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), // Future
		ExtendedSupport: true,
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.NotNil(t, lifecycle.ExtendedSupportEnd)
	// When ExtendedSupport is boolean true, use EOL date
	assert.Equal(t, lifecycle.EOLDate, lifecycle.ExtendedSupportEnd)
}

func TestStandardSchemaAdapter_FalseBooleans(t *testing.T) {
	adapter := &StandardSchemaAdapter{}

	// EOL and Support as "false" strings
	cycle := &ProductCycle{
		Cycle:       "17.0",
		ReleaseDate: "2024-06-15",
		Support:     "false",
		EOL:         "false",
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.Nil(t, lifecycle.DeprecationDate)
	assert.Nil(t, lifecycle.EOLDate)
	assert.True(t, lifecycle.IsSupported)
	assert.False(t, lifecycle.IsDeprecated)
}

// TestStandardSchemaAdapter_AWSPattern_InExtendedSupport pins the
// amazon-elasticache-redis cycle 5/6 shape: no `support` field,
// `eol` is the end of standard support (NOT terminal), and
// `extendedSupport` is the real terminal date. A version past `eol`
// but before `extendedSupport` must classify as in-extended-support
// (YELLOW), not EOL (RED).
func TestStandardSchemaAdapter_AWSPattern_InExtendedSupport(t *testing.T) {
	adapter := &StandardSchemaAdapter{}

	pastYear := time.Now().Year() - 1
	futureYear := time.Now().Year() + 2
	cycle := &ProductCycle{
		Cycle:           "5",
		ReleaseDate:     "2018-10-17",
		EOL:             time.Date(pastYear, 1, 31, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),   // standard-support end (past)
		ExtendedSupport: time.Date(futureYear, 1, 31, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), // extended-support end (future)
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.True(t, lifecycle.IsSupported)
	assert.True(t, lifecycle.IsDeprecated)
	assert.True(t, lifecycle.IsExtendedSupport)
	assert.False(t, lifecycle.IsEOL)
	// True EOL = end of extended support, not the renamed `eol`.
	assert.NotNil(t, lifecycle.EOLDate)
	assert.NotNil(t, lifecycle.ExtendedSupportEnd)
	assert.Equal(t, *lifecycle.EOLDate, *lifecycle.ExtendedSupportEnd)
	// DeprecationDate = `eol` (since there's no `support` field).
	assert.NotNil(t, lifecycle.DeprecationDate)
}

// TestStandardSchemaAdapter_AWSPattern_PastExtendedSupport: same shape
// as above but past extendedSupport too — true EOL.
func TestStandardSchemaAdapter_AWSPattern_PastExtendedSupport(t *testing.T) {
	adapter := &StandardSchemaAdapter{}

	cycle := &ProductCycle{
		Cycle:           "3",
		EOL:             "2020-01-31", // standard-support end (past)
		ExtendedSupport: "2023-01-31", // extended-support end (past)
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.True(t, lifecycle.IsEOL)
	assert.False(t, lifecycle.IsSupported)
	assert.True(t, lifecycle.IsDeprecated)
}

// TestStandardSchemaAdapter_AWSPattern_StillStandard: AWS pattern
// with both eol and extendedSupport in the future → standard support.
func TestStandardSchemaAdapter_AWSPattern_StillStandard(t *testing.T) {
	adapter := &StandardSchemaAdapter{}

	futureYear := time.Now().Year() + 1
	cycle := &ProductCycle{
		Cycle:           "6",
		EOL:             time.Date(futureYear, 1, 31, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),   // future
		ExtendedSupport: time.Date(futureYear+3, 1, 31, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), // future
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.True(t, lifecycle.IsSupported)
	assert.False(t, lifecycle.IsDeprecated)
	assert.False(t, lifecycle.IsExtendedSupport)
	assert.False(t, lifecycle.IsEOL)
}

// TestStandardSchemaAdapter_ExtendedSupportOverridesPastEOL guards the
// reordering: previously, when `eol` was past, the EOL branch returned
// before the extended-support branch could fire, so a future
// `extendedSupport` date was ignored. This test pins that a future
// extendedSupport date now correctly extends the version's life.
func TestStandardSchemaAdapter_ExtendedSupportOverridesPastEOL(t *testing.T) {
	adapter := &StandardSchemaAdapter{}

	pastYear := time.Now().Year() - 2
	futureYear := time.Now().Year() + 2
	cycle := &ProductCycle{
		Cycle:           "5.6",
		Support:         time.Date(pastYear, 2, 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),
		EOL:             time.Date(pastYear, 2, 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),
		ExtendedSupport: time.Date(futureYear, 2, 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.False(t, lifecycle.IsEOL)
	assert.True(t, lifecycle.IsExtendedSupport)
	assert.True(t, lifecycle.IsSupported)
	assert.True(t, lifecycle.IsDeprecated)
}

func eksDeclarativeAdapter(t *testing.T) *DeclarativeSchemaAdapter {
	t.Helper()

	adapter, err := NewDeclarativeSchemaAdapter(&DeclarativeLifecycleConfig{
		DeprecationDate: LifecycleDateSource{Field: lifecycleFieldEOL},
		ExtendedSupportEnd: LifecycleDateSource{
			Field:            lifecycleFieldExtendedSupport,
			BoolTrueFallback: lifecycleFieldEOL,
		},
		EOLDate:             LifecycleDateSource{Field: lifecycleFieldExtendedSupport},
		DeprecatedWindow:    lifecycleActionExtendedSupport,
		PastExtendedSupport: lifecycleActionUnsupported,
	})
	require.NoError(t, err)
	return adapter
}

func lambdaActionableEOLAdapter(t *testing.T) *DeclarativeSchemaAdapter {
	t.Helper()

	adapter, err := NewDeclarativeSchemaAdapter(&DeclarativeLifecycleConfig{
		DeprecationDate:    LifecycleDateSource{Field: lifecycleFieldSupport},
		ExtendedSupportEnd: LifecycleDateSource{Field: lifecycleFieldEOL},
		EOLDate:            LifecycleDateSource{Field: lifecycleFieldSupport},
	})
	require.NoError(t, err)
	return adapter
}

func TestStandardSchemaAdapter_LambdaDeprecatedSupportWindow(t *testing.T) {
	adapter := &StandardSchemaAdapter{}

	pastYear := time.Now().Year() - 1
	futureYear := time.Now().Year() + 1
	cycle := &ProductCycle{
		Cycle:       "python3.8",
		ReleaseDate: "2019-11-18",
		Support:     time.Date(pastYear, 10, 14, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),
		EOL:         time.Date(futureYear, 9, 30, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.Equal(t, "python3.8", lifecycle.Version)
	assert.Empty(t, lifecycle.Engine)
	assert.True(t, lifecycle.IsSupported)
	assert.True(t, lifecycle.IsDeprecated)
	assert.True(t, lifecycle.IsDeprecatedSupport)
	assert.False(t, lifecycle.IsExtendedSupport)
	assert.False(t, lifecycle.IsEOL)
	assert.NotNil(t, lifecycle.DeprecationDate)
	assert.Nil(t, lifecycle.ExtendedSupportEnd)
	assert.NotNil(t, lifecycle.EOLDate)
}

func TestStandardSchemaAdapter_LambdaPastDeprecatedSupport(t *testing.T) {
	adapter := &StandardSchemaAdapter{}

	cycle := &ProductCycle{
		Cycle:       "nodejs12.x",
		ReleaseDate: "2019-11-18",
		Support:     "2023-03-31",
		EOL:         "2023-04-30",
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.False(t, lifecycle.IsSupported)
	assert.True(t, lifecycle.IsDeprecated)
	assert.False(t, lifecycle.IsDeprecatedSupport)
	assert.False(t, lifecycle.IsExtendedSupport)
	assert.True(t, lifecycle.IsEOL)
}

func TestStandardSchemaAdapter_LambdaCurrentStandardSupport(t *testing.T) {
	adapter := &StandardSchemaAdapter{}

	futureYear := time.Now().Year() + 1
	cycle := &ProductCycle{
		Cycle:       "python3.13",
		ReleaseDate: "2024-11-14",
		Support:     time.Date(futureYear, 6, 30, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),
		EOL:         time.Date(futureYear+1, 8, 31, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.True(t, lifecycle.IsSupported)
	assert.False(t, lifecycle.IsDeprecated)
	assert.False(t, lifecycle.IsExtendedSupport)
	assert.False(t, lifecycle.IsEOL)
}

func TestDeclarativeSchemaAdapter_LambdaUsesSupportAsActionableEOL(t *testing.T) {
	adapter := lambdaActionableEOLAdapter(t)

	support := time.Now().AddDate(0, 0, 60)
	terminal := time.Now().AddDate(1, 0, 0)
	cycle := &ProductCycle{
		Cycle:       "python3.10",
		ReleaseDate: "2023-04-18",
		Support:     support.Format("2006-01-02"),
		EOL:         terminal.Format("2006-01-02"),
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	require.NotNil(t, lifecycle.EOLDate)
	require.NotNil(t, lifecycle.DeprecationDate)
	require.NotNil(t, lifecycle.ExtendedSupportEnd)
	assert.Equal(t, support.Format("2006-01-02"), lifecycle.EOLDate.Format("2006-01-02"))
	assert.Equal(t, support.Format("2006-01-02"), lifecycle.DeprecationDate.Format("2006-01-02"))
	assert.Equal(t, terminal.Format("2006-01-02"), lifecycle.ExtendedSupportEnd.Format("2006-01-02"))
	assert.True(t, lifecycle.IsSupported)
	assert.False(t, lifecycle.IsDeprecated)
	assert.False(t, lifecycle.IsEOL)
}

func TestDeclarativeSchemaAdapter_LambdaPastActionableEOLIsEOL(t *testing.T) {
	adapter := lambdaActionableEOLAdapter(t)

	cycle := &ProductCycle{
		Cycle:       "python3.8",
		ReleaseDate: "2019-11-18",
		Support:     time.Now().AddDate(0, -1, 0).Format("2006-01-02"),
		EOL:         time.Now().AddDate(1, 0, 0).Format("2006-01-02"),
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.False(t, lifecycle.IsSupported)
	assert.True(t, lifecycle.IsDeprecated)
	assert.True(t, lifecycle.IsEOL)
	assert.False(t, lifecycle.IsDeprecatedSupport)
	assert.False(t, lifecycle.IsExtendedSupport)
}

func TestDeclarativeSchemaAdapter_EKSCurrentVersion(t *testing.T) {
	adapter := eksDeclarativeAdapter(t)

	// Live amazon-eks shape: cycle.eol is end-of-standard-support and
	// cycle.extendedSupport is end-of-extended-support. Both in the
	// future → standard support today.
	futureYear := time.Now().Year() + 1
	cycle := &ProductCycle{
		Cycle:           "1.31",
		ReleaseDate:     "2024-11-15",
		EOL:             time.Date(futureYear, 11, 15, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),   // standard-support end (future)
		ExtendedSupport: time.Date(futureYear+1, 11, 15, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), // extended-support end (future)
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.Equal(t, "1.31", lifecycle.Version)
	assert.Empty(t, lifecycle.Engine)
	assert.True(t, lifecycle.IsSupported)
	assert.False(t, lifecycle.IsDeprecated)
	assert.False(t, lifecycle.IsEOL)
	assert.False(t, lifecycle.IsExtendedSupport)

	// EKS true EOL is the end of extended support.
	assert.NotNil(t, lifecycle.EOLDate)
	// DeprecationDate = cycle.eol (end of standard support).
	assert.NotNil(t, lifecycle.DeprecationDate)
	// ExtendedSupportEnd and EOLDate = cycle.extendedSupport.
	assert.NotNil(t, lifecycle.ExtendedSupportEnd)
	assert.Equal(t, *lifecycle.ExtendedSupportEnd, *lifecycle.EOLDate)
}

func TestDeclarativeSchemaAdapter_EKSInExtendedSupport(t *testing.T) {
	adapter := eksDeclarativeAdapter(t)

	// Past cycle.eol (end of standard support) but before cycle.extendedSupport
	// (end of extended support) → IN extended support → YELLOW.
	pastYear := time.Now().Year() - 1
	futureYear := time.Now().Year() + 1
	cycle := &ProductCycle{
		Cycle:           "1.30",
		ReleaseDate:     "2024-05-23",
		EOL:             time.Date(pastYear, 7, 23, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),   // standard-support end (past)
		ExtendedSupport: time.Date(futureYear, 7, 23, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), // extended-support end (future)
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.True(t, lifecycle.IsSupported)
	assert.True(t, lifecycle.IsDeprecated)
	assert.False(t, lifecycle.IsEOL)
	assert.True(t, lifecycle.IsExtendedSupport)
	assert.NotNil(t, lifecycle.EOLDate)
}

func TestDeclarativeSchemaAdapter_EKSPastExtendedSupport(t *testing.T) {
	adapter := eksDeclarativeAdapter(t)

	// Past both cycle.eol AND cycle.extendedSupport — AWS no longer patches.
	cycle := &ProductCycle{
		Cycle:           "1.25",
		ReleaseDate:     "2023-01-15",
		EOL:             "2024-01-15", // standard-support end (past)
		ExtendedSupport: "2024-07-15", // extended-support end (past)
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.False(t, lifecycle.IsSupported)
	assert.True(t, lifecycle.IsDeprecated)
	assert.True(t, lifecycle.IsEOL)
	assert.False(t, lifecycle.IsExtendedSupport)
}

func TestDeclarativeSchemaAdapter_EKSEOLIsExtendedSupportEnd(t *testing.T) {
	adapter := eksDeclarativeAdapter(t)

	cycle := &ProductCycle{
		Cycle:           "1.20",
		ReleaseDate:     "2021-01-15",
		EOL:             "2022-01-15",
		ExtendedSupport: "2022-07-15",
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	// Verify EKS true EOL is the end of extended support.
	assert.NotNil(t, lifecycle.EOLDate)
	expectedDate, _ := time.Parse("2006-01-02", "2022-07-15")
	assert.Equal(t, expectedDate, *lifecycle.EOLDate)

	// ExtendedSupportEnd comes from cycle.extendedSupport (NOT cycle.eol).
	assert.NotNil(t, lifecycle.ExtendedSupportEnd)
	assert.Equal(t, expectedDate, *lifecycle.ExtendedSupportEnd)

	// DeprecationDate comes from cycle.eol (end of standard support).
	assert.NotNil(t, lifecycle.DeprecationDate)
	expectedStd, _ := time.Parse("2006-01-02", "2022-01-15")
	assert.Equal(t, expectedStd, *lifecycle.DeprecationDate)
}

// TestDeclarativeSchemaAdapter_EKSLegacyBooleanExtendedSupport guards the
// pre-2026 amazon-eks shape where cycle.extendedSupport was a boolean.
// Live data now uses dates, but the YAML bool_true_fallback still tolerates the
// legacy boolean so a hypothetical replay against archived JSON
// classifies clusters consistently.
func TestDeclarativeSchemaAdapter_EKSLegacyBooleanExtendedSupport(t *testing.T) {
	adapter := eksDeclarativeAdapter(t)

	// Past cycle.eol with extendedSupport=true bool — the bool falls
	// back to standardEnd as the extended-support boundary, so we
	// land in past-extended → IsDeprecated, !IsExtendedSupport.
	cycle := &ProductCycle{
		Cycle:           "1.24",
		ReleaseDate:     "2022-08-15",
		EOL:             "2024-01-15", // past
		ExtendedSupport: true,         // legacy bool
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.False(t, lifecycle.IsSupported)
	assert.True(t, lifecycle.IsDeprecated)
	assert.False(t, lifecycle.IsExtendedSupport)
	// Legacy boolean extendedSupport has no terminal date, so EOLDate stays nil
	// rather than using cycle.eol (standard-support end) as true EOL.
	assert.Nil(t, lifecycle.EOLDate)
}

func TestGetSchemaAdapter_Standard(t *testing.T) {
	adapter, err := GetSchemaAdapter("standard")
	require.NoError(t, err)
	assert.IsType(t, &StandardSchemaAdapter{}, adapter)
}

func TestNewDeclarativeSchemaAdapter_Validation(t *testing.T) {
	adapter, err := NewDeclarativeSchemaAdapter(&DeclarativeLifecycleConfig{
		DeprecationDate:  LifecycleDateSource{Field: lifecycleFieldSupport},
		DeprecatedWindow: lifecycleActionExtendedSupport,
	})
	require.NoError(t, err)
	assert.IsType(t, &DeclarativeSchemaAdapter{}, adapter)

	_, err = NewDeclarativeSchemaAdapter(&DeclarativeLifecycleConfig{
		DeprecationDate: LifecycleDateSource{Field: "unsupportedField"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported field")
}

func TestGetSchemaAdapter_Unknown(t *testing.T) {
	adapter, err := GetSchemaAdapter("unknown_adapter")
	assert.Error(t, err)
	assert.Nil(t, adapter)
	assert.Contains(t, err.Error(), "unknown schema adapter")
}

func TestStandardSchemaAdapter_EmptyDates(t *testing.T) {
	adapter := &StandardSchemaAdapter{}

	cycle := &ProductCycle{
		Cycle:       "18.0",
		ReleaseDate: "",
		Support:     "",
		EOL:         "",
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.Nil(t, lifecycle.ReleaseDate)
	assert.Nil(t, lifecycle.DeprecationDate)
	assert.Nil(t, lifecycle.EOLDate)

	// Without dates, should still be considered supported
	assert.True(t, lifecycle.IsSupported)
	assert.False(t, lifecycle.IsDeprecated)
}

func TestDeclarativeSchemaAdapter_EmptyDates(t *testing.T) {
	adapter := eksDeclarativeAdapter(t)

	cycle := &ProductCycle{
		Cycle:       "1.32",
		ReleaseDate: "",
		Support:     "",
		EOL:         "",
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.Nil(t, lifecycle.ReleaseDate)
	assert.Nil(t, lifecycle.DeprecationDate)
	assert.Nil(t, lifecycle.ExtendedSupportEnd)
	assert.Nil(t, lifecycle.EOLDate)

	assert.True(t, lifecycle.IsSupported)
	assert.False(t, lifecycle.IsDeprecated)
}
