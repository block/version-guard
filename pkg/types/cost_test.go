package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLifecycleDetailsUsesEOLAsStandardSupportEndWhenExtendedSupportExists(t *testing.T) {
	eolDate := time.Date(2024, 10, 31, 0, 0, 0, 0, time.UTC)
	extendedSupportEnd := time.Date(2027, 2, 28, 0, 0, 0, 0, time.UTC)

	details := LifecycleDetailsFromVersionLifecycle(&VersionLifecycle{
		Version:            "2",
		EOLDate:            &eolDate,
		ExtendedSupportEnd: &extendedSupportEnd,
		IsExtendedSupport:  true,
	})

	require.NotNil(t, details)
	require.NotNil(t, details.StandardSupportEnd)
	assert.Equal(t, eolDate, *details.StandardSupportEnd)
}
