package endoflife

import (
	"context"
)

// MockClient is a mock implementation of Client for testing
type MockClient struct {
	GetProductCyclesFunc func(ctx context.Context, product string) (ProductCyclesResult, error)
}

// GetProductCycles calls the mock function
func (m *MockClient) GetProductCycles(ctx context.Context, product string) (ProductCyclesResult, error) {
	if m.GetProductCyclesFunc != nil {
		return m.GetProductCyclesFunc(ctx, product)
	}
	return ProductCyclesResult{}, nil
}
