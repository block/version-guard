package wiz

// TagConfig defines the AWS tag keys to use when extracting metadata from resources.
// This allows users to customize tag naming conventions to match their organization.
type TagConfig struct {
	// AppTags are the possible tag keys for application/service name
	// Checked in order; first match wins
	AppTags []string

	// EnvTags are the possible tag keys for environment
	// Checked in order; first match wins
	EnvTags []string

	// BrandTags are the possible tag keys for brand/business unit
	// Checked in order; first match wins
	BrandTags []string
}

// DefaultTagConfig returns the default tag configuration with common AWS tag naming conventions.
// Users can customize these by creating their own TagConfig when initializing inventory sources.
//
// Example:
//
//	customTags := &wiz.TagConfig{
//	    AppTags:   []string{"my-app-tag", "application"},
//	    EnvTags:   []string{"environment"},
//	    BrandTags: []string{"cost-center", "department"},
//	}
//	source := wiz.NewAuroraInventorySource(client, reportID).WithTagConfig(customTags)
func DefaultTagConfig() *TagConfig {
	return &TagConfig{
		AppTags:   []string{"app", "application", "service"},
		EnvTags:   []string{"environment", "env"},
		BrandTags: []string{"brand"},
	}
}

// GetTagValue searches for the first matching tag key in the tags map.
// Returns the tag value if found, empty string otherwise.
func (tc *TagConfig) GetAppTag(tags map[string]string) string {
	return getFirstMatchingTag(tags, tc.AppTags)
}

// GetEnvTag returns the environment tag value if found.
func (tc *TagConfig) GetEnvTag(tags map[string]string) string {
	return getFirstMatchingTag(tags, tc.EnvTags)
}

// GetBrandTag returns the brand tag value if found.
func (tc *TagConfig) GetBrandTag(tags map[string]string) string {
	return getFirstMatchingTag(tags, tc.BrandTags)
}

// getFirstMatchingTag searches for the first matching tag key in order.
func getFirstMatchingTag(tags map[string]string, keys []string) string {
	for _, key := range keys {
		if value, exists := tags[key]; exists && value != "" {
			return value
		}
	}
	return ""
}
