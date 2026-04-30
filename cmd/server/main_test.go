package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildMetricTagsDefaultsService(t *testing.T) {
	t.Setenv("DD_SERVICE", "")
	t.Setenv("DD_ENV", "")
	t.Setenv("DD_VERSION", "")

	assert.Equal(t, []string{"service:version-guard"}, buildMetricTags(""))
}

func TestBuildMetricTagsUsesDatadogEnvVars(t *testing.T) {
	t.Setenv("DD_SERVICE", "custom-service")
	t.Setenv("DD_ENV", "staging")
	t.Setenv("DD_VERSION", "1.2.3")

	assert.Equal(t, []string{
		"team:platform",
		"service:custom-service",
		"env:staging",
		"version:1.2.3",
	}, buildMetricTags("team:platform"))
}

func TestBuildMetricTagsDoesNotOverrideExplicitTags(t *testing.T) {
	t.Setenv("DD_SERVICE", "env-service")
	t.Setenv("DD_ENV", "prod")
	t.Setenv("DD_VERSION", "1.2.3")

	assert.Equal(t, []string{
		"service:explicit-service",
		"env:explicit-env",
		"version:explicit-version",
	}, buildMetricTags("service:explicit-service,env:explicit-env,version:explicit-version"))
}

func TestDogStatsDAddrDefaults(t *testing.T) {
	t.Setenv("DD_AGENT_HOST", "")
	t.Setenv("DD_DOGSTATSD_PORT", "")

	assert.Equal(t, "127.0.0.1:8125", dogStatsDAddr(""))
}

func TestDogStatsDAddrUsesDatadogEnvVars(t *testing.T) {
	t.Setenv("DD_AGENT_HOST", "datadog-agent")
	t.Setenv("DD_DOGSTATSD_PORT", "8126")

	assert.Equal(t, "datadog-agent:8126", dogStatsDAddr(""))
}
