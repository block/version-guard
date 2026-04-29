package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/block/Version-Guard/pkg/types"
)

func TestWorkflowInput_DefaultResourceTypes(t *testing.T) {
	input := WorkflowInput{
		ScanID:        "test-scan-1",
		ResourceTypes: []types.ResourceType{},
	}

	// Test that empty resource types will be populated by workflow
	assert.Equal(t, "test-scan-1", input.ScanID)
	assert.Empty(t, input.ResourceTypes)
}

func TestWorkflowOutput_Structure(t *testing.T) {
	output := WorkflowOutput{
		ScanID:               "test-scan-1",
		SnapshotID:           "test-snapshot-1",
		TotalFindings:        100,
		CompliancePercentage: 85.5,
		ResourceTypeResults:  make(map[types.ResourceType]*ResourceTypeResult),
	}

	assert.Equal(t, "test-scan-1", output.ScanID)
	assert.Equal(t, "test-snapshot-1", output.SnapshotID)
	assert.Equal(t, 100, output.TotalFindings)
	assert.Equal(t, 85.5, output.CompliancePercentage)
	assert.NotNil(t, output.ResourceTypeResults)
}

func TestResourceTypeResult_Structure(t *testing.T) {
	result := ResourceTypeResult{
		ResourceType:   types.ResourceTypeAurora,
		FindingsCount:  50,
		RedCount:       5,
		YellowCount:    10,
		GreenCount:     35,
		UnknownCount:   0,
		DurationMillis: 5000,
		Error:          "",
	}

	assert.Equal(t, types.ResourceTypeAurora, result.ResourceType)
	assert.Equal(t, 50, result.FindingsCount)
	assert.Equal(t, 5, result.RedCount)
	assert.Equal(t, 10, result.YellowCount)
	assert.Equal(t, 35, result.GreenCount)
	assert.Equal(t, 0, result.UnknownCount)
	assert.Equal(t, int64(5000), result.DurationMillis)
	assert.Empty(t, result.Error)
}
