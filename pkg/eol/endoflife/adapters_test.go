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

func TestStandardSchemaAdapter_DeprecatedVersion(t *testing.T) {
	adapter := &StandardSchemaAdapter{}

	// Version past standard support but before EOL
	cycle := &ProductCycle{
		Cycle:       "15.0",
		ReleaseDate: "2023-01-15",
		Support:     "2024-01-15", // Past
		EOL:         "2028-01-15", // Future
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.False(t, lifecycle.IsSupported)
	assert.True(t, lifecycle.IsDeprecated)
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

func TestEKSSchemaAdapter_CurrentVersion(t *testing.T) {
	adapter := &EKSSchemaAdapter{}

	futureYear := time.Now().Year() + 1
	cycle := &ProductCycle{
		Cycle:       "1.31",
		ReleaseDate: "2024-11-15",
		Support:     time.Date(futureYear, 11, 15, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),  // Future
		EOL:         time.Date(futureYear+1, 5, 15, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), // Future (extended support end in EKS)
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.Equal(t, "1.31", lifecycle.Version)
	assert.Equal(t, "eks", lifecycle.Engine)
	assert.True(t, lifecycle.IsSupported)
	assert.False(t, lifecycle.IsDeprecated)
	assert.False(t, lifecycle.IsEOL)
	assert.False(t, lifecycle.IsExtendedSupport)

	// EKS has NO true EOL
	assert.Nil(t, lifecycle.EOLDate)
	assert.NotNil(t, lifecycle.DeprecationDate)
	assert.NotNil(t, lifecycle.ExtendedSupportEnd)
}

func TestEKSSchemaAdapter_InExtendedSupport(t *testing.T) {
	adapter := &EKSSchemaAdapter{}

	// Version past standard support but in extended support
	pastYear := time.Now().Year() - 1
	futureYear := time.Now().Year() + 1
	cycle := &ProductCycle{
		Cycle:       "1.28",
		ReleaseDate: "2023-09-15",
		Support:     time.Date(pastYear, 9, 15, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),   // Past
		EOL:         time.Date(futureYear, 3, 15, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), // Future (extended support end)
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.True(t, lifecycle.IsSupported)
	assert.True(t, lifecycle.IsDeprecated)
	assert.False(t, lifecycle.IsEOL) // EKS never truly EOL
	assert.True(t, lifecycle.IsExtendedSupport)
}

func TestEKSSchemaAdapter_PastExtendedSupport(t *testing.T) {
	adapter := &EKSSchemaAdapter{}

	// Version past extended support end
	cycle := &ProductCycle{
		Cycle:       "1.25",
		ReleaseDate: "2023-01-15",
		Support:     "2024-01-15", // Past
		EOL:         "2024-07-15", // Past (extended support end)
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	assert.False(t, lifecycle.IsSupported)
	assert.True(t, lifecycle.IsDeprecated)
	assert.False(t, lifecycle.IsEOL) // EKS clusters keep running
	assert.False(t, lifecycle.IsExtendedSupport)
}

func TestEKSSchemaAdapter_NoTrueEOL(t *testing.T) {
	adapter := &EKSSchemaAdapter{}

	cycle := &ProductCycle{
		Cycle:       "1.20",
		ReleaseDate: "2021-01-15",
		Support:     "2022-01-15",
		EOL:         "2022-07-15",
	}

	lifecycle, err := adapter.AdaptCycle(cycle)
	require.NoError(t, err)

	// Verify EKS has NO true EOL date
	assert.Nil(t, lifecycle.EOLDate)

	// ExtendedSupportEnd comes from cycle.EOL
	assert.NotNil(t, lifecycle.ExtendedSupportEnd)
	expectedDate, _ := time.Parse("2006-01-02", "2022-07-15")
	assert.Equal(t, expectedDate, *lifecycle.ExtendedSupportEnd)
}

func TestGetSchemaAdapter_Standard(t *testing.T) {
	adapter, err := GetSchemaAdapter("standard")
	require.NoError(t, err)
	assert.IsType(t, &StandardSchemaAdapter{}, adapter)
}

func TestGetSchemaAdapter_EKS(t *testing.T) {
	adapter, err := GetSchemaAdapter("eks_adapter")
	require.NoError(t, err)
	assert.IsType(t, &EKSSchemaAdapter{}, adapter)
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

func TestEKSSchemaAdapter_EmptyDates(t *testing.T) {
	adapter := &EKSSchemaAdapter{}

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
