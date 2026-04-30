package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewTemporalMetricsHandlerRequiresListenAddress(t *testing.T) {
	handler, closer, err := newTemporalMetricsHandler(" ")
	require.Error(t, err)
	require.Nil(t, handler)
	require.Nil(t, closer)
	require.Contains(t, err.Error(), "listen address is required")
}

func TestNewTemporalMetricsHandlerCreatesHandler(t *testing.T) {
	handler, closer, err := newTemporalMetricsHandler("127.0.0.1:0")
	require.NoError(t, err)
	require.NotNil(t, handler)
	require.NotNil(t, closer)
	require.NoError(t, closer.Close())
}

func TestNewTemporalMetricsHandlerReturnsListenErrors(t *testing.T) {
	handler, closer, err := newTemporalMetricsHandler("127.0.0.1:not-a-port")
	require.Error(t, err)
	require.Nil(t, handler)
	require.Nil(t, closer)
	require.Contains(t, err.Error(), "listen for temporal metrics")
}
