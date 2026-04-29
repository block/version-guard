package cost

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/Version-Guard/pkg/types"
)

type fakeCheck struct {
	id        string
	applies   bool
	estimate  *types.CostEstimate
	estimateE error
}

func (c fakeCheck) ID() string {
	return c.id
}

func (c fakeCheck) AppliesTo(_ *types.Finding) bool {
	return c.applies
}

func (c fakeCheck) Estimate(_ context.Context, _ CostInput) (*types.CostEstimate, error) {
	return c.estimate, c.estimateE
}

func TestRunnerAppliesRegisteredChecksAndAggregatesSummary(t *testing.T) {
	snapshot := &types.Snapshot{
		FindingsByType: map[types.ResourceType][]*types.Finding{
			"aurora-mysql": {
				{
					ResourceID:   "arn:aws:rds:us-east-1:123:cluster:prod",
					ResourceType: "aurora-mysql",
					Service:      "payments",
					Extra: map[string]string{
						"brand":      "cash",
						"account_id": "123",
					},
				},
			},
		},
	}

	runner := NewRunner(
		WithChecks(fakeCheck{
			id:      "aws.aurora_mysql.extended_support_surcharge",
			applies: true,
			estimate: &types.CostEstimate{
				CheckID:    "aws.aurora_mysql.extended_support_surcharge",
				Status:     types.CostEstimateStatusAvailable,
				Basis:      types.CostEstimateBasisCurrent,
				HourlyUSD:  0.60,
				MonthlyUSD: 438.00,
				AnnualUSD:  5256.00,
			},
		}),
	)

	enriched, err := runner.Enrich(context.Background(), snapshot)
	require.NoError(t, err)

	findings := enriched.FindingsByType["aurora-mysql"]
	require.Len(t, findings, 1)
	require.Len(t, findings[0].CostEstimates, 1)
	assert.Equal(t, "aws.aurora_mysql.extended_support_surcharge", findings[0].CostEstimates[0].CheckID)

	require.NotNil(t, enriched.Summary.CostSummary)
	assert.InDelta(t, 438.00, enriched.Summary.CostSummary.CurrentMonthlyUSD, 0.001)
	assert.InDelta(t, 5256.00, enriched.Summary.CostSummary.CurrentAnnualUSD, 0.001)
	assert.Equal(t, 1, enriched.Summary.CostSummary.AvailableCount)
	assert.Equal(t, 1, enriched.Summary.CostSummary.ByService["payments"].AvailableCount)
	assert.Equal(t, 1, enriched.Summary.CostSummary.ByBrand["cash"].AvailableCount)
	assert.Equal(t, 1, enriched.Summary.CostSummary.ByAccount["123"].AvailableCount)
	assert.Equal(t, 1, enriched.Summary.CostSummary.ByCheckID["aws.aurora_mysql.extended_support_surcharge"].AvailableCount)
}

func TestRunnerPreservesFindingsWhenCheckFails(t *testing.T) {
	snapshot := &types.Snapshot{
		FindingsByType: map[types.ResourceType][]*types.Finding{
			"aurora-mysql": {
				{
					ResourceID:   "arn:aws:rds:us-east-1:123:cluster:prod",
					ResourceType: "aurora-mysql",
				},
			},
		},
	}

	runner := NewRunner(
		WithChecks(fakeCheck{
			id:        "failing.check",
			applies:   true,
			estimateE: errors.New("pricing backend unavailable"),
		}),
	)

	enriched, err := runner.Enrich(context.Background(), snapshot)
	require.NoError(t, err)

	findings := enriched.FindingsByType["aurora-mysql"]
	require.Len(t, findings, 1)
	require.Len(t, findings[0].CostEstimates, 1)
	assert.Equal(t, types.CostEstimateStatusInsufficientData, findings[0].CostEstimates[0].Status)
	assert.Contains(t, findings[0].CostEstimates[0].MissingInputs, "estimation_error")
	assert.Equal(t, "pricing backend unavailable", findings[0].CostEstimates[0].Assumptions[0])
}

func TestRunnerRegistersMultipleChecks(t *testing.T) {
	snapshot := &types.Snapshot{
		FindingsByType: map[types.ResourceType][]*types.Finding{
			"aurora-mysql": {
				{ResourceID: "arn:aws:rds:us-east-1:123:cluster:prod", ResourceType: "aurora-mysql"},
			},
		},
	}

	runner := NewRunner(
		WithChecks(
			fakeCheck{
				id:      "current.check",
				applies: true,
				estimate: &types.CostEstimate{
					Status:     types.CostEstimateStatusAvailable,
					Basis:      types.CostEstimateBasisCurrent,
					MonthlyUSD: 10,
					AnnualUSD:  120,
				},
			},
			fakeCheck{
				id:      "projected.check",
				applies: true,
				estimate: &types.CostEstimate{
					Status:     types.CostEstimateStatusAvailable,
					Basis:      types.CostEstimateBasisProjected,
					MonthlyUSD: 20,
					AnnualUSD:  240,
				},
			},
		),
	)

	enriched, err := runner.Enrich(context.Background(), snapshot)
	require.NoError(t, err)

	finding := enriched.FindingsByType["aurora-mysql"][0]
	require.Len(t, finding.CostEstimates, 2)
	assert.Equal(t, "current.check", finding.CostEstimates[0].CheckID)
	assert.Equal(t, "projected.check", finding.CostEstimates[1].CheckID)
	assert.InDelta(t, 10.0, enriched.Summary.CostSummary.CurrentMonthlyUSD, 0.001)
	assert.InDelta(t, 20.0, enriched.Summary.CostSummary.ProjectedMonthlyUSD, 0.001)
}
