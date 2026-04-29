package aws

import "fmt"

// VCPUResolver maps DB instance classes to vCPU counts.
type VCPUResolver struct {
	byInstanceClass map[string]int
}

// NewVCPUResolver builds a resolver from AmazonRDS price list rows.
func NewVCPUResolver(rows []PriceListRow) *VCPUResolver {
	byClass := make(map[string]int)
	for _, row := range rows {
		if row.InstanceType == "" || row.VCPU <= 0 {
			continue
		}
		byClass[row.InstanceType] = row.VCPU
	}
	return &VCPUResolver{byInstanceClass: byClass}
}

// Resolve returns the vCPU count for an instance class.
func (r *VCPUResolver) Resolve(instanceClass string) (int, error) {
	if r == nil {
		return 0, fmt.Errorf("vcpu resolver is nil")
	}
	vcpu, ok := r.byInstanceClass[instanceClass]
	if !ok || vcpu <= 0 {
		return 0, fmt.Errorf("unknown DB instance class %q", instanceClass)
	}
	return vcpu, nil
}
