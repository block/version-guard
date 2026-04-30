package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/uber-go/tally/v4"
	tallyprom "github.com/uber-go/tally/v4/prometheus"
	"go.temporal.io/sdk/client"
	sdktally "go.temporal.io/sdk/contrib/tally"
)

type temporalMetricsCloser struct {
	server      *http.Server
	scopeCloser io.Closer
}

func (c *temporalMetricsCloser) Close() error {
	var closeErr error
	if c.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			closeErr = errors.Join(closeErr, err)
		}
	}
	if c.scopeCloser != nil {
		closeErr = errors.Join(closeErr, c.scopeCloser.Close())
	}
	return closeErr
}

func newTemporalMetricsHandler(listenAddress string) (client.MetricsHandler, io.Closer, error) {
	listenAddress = strings.TrimSpace(listenAddress)
	if listenAddress == "" {
		return nil, nil, fmt.Errorf("temporal metrics listen address is required")
	}

	registry := prom.NewRegistry()
	reporter := tallyprom.NewReporter(tallyprom.Options{
		Registerer:       registry,
		Gatherer:         registry,
		DefaultTimerType: tallyprom.HistogramTimerType,
		OnRegisterError: func(err error) {
			slog.Warn("temporal metrics reporter error", "error", err)
		},
	})

	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return nil, nil, fmt.Errorf("listen for temporal metrics on %s: %w", listenAddress, err)
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", reporter.HTTPHandler())
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("temporal metrics server stopped", "error", err)
		}
	}()

	scopeOpts := tally.ScopeOptions{
		CachedReporter:  reporter,
		Separator:       tallyprom.DefaultSeparator,
		SanitizeOptions: &sdktally.PrometheusSanitizeOptions,
	}
	scope, scopeCloser := tally.NewRootScope(scopeOpts, time.Second)
	scope = sdktally.NewPrometheusNamingScope(scope)
	return sdktally.NewMetricsHandler(scope), &temporalMetricsCloser{
		server:      server,
		scopeCloser: scopeCloser,
	}, nil
}
