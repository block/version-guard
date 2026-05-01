package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"
)

// NotifyEmitterActivityName is the registered name of the Stage 3 webhook
// activity that pings the downstream emitter once the snapshot is in S3.
const NotifyEmitterActivityName = "version-guard.NotifyEmitter"

// NotifyEmitterInput is the activity input. EmitterWebhookURL is the base
// URL of the emitter's admin HTTP server (e.g. http://version-guard-emitter:8080);
// the activity appends "/trigger-act".
type NotifyEmitterInput struct {
	EmitterWebhookURL string
	SnapshotID        string
}

// NotifyEmitterResult mirrors the emitter's /trigger-act response so the
// workflow history records which downstream execution was started.
type NotifyEmitterResult struct {
	WorkflowID string
	RunID      string
	SnapshotID string
}

// HTTPDoer is the subset of *http.Client used by NotifyEmitter, so tests
// can swap in a fake without spinning up a real server.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// NotifyEmitter POSTs the snapshot id to the emitter's webhook so it can
// start its ActWorkflow. Returns the started workflow's identifiers.
//
// The activity is intentionally short-timeout / retry-friendly: the
// snapshot is already durable in S3, so a transient emitter outage just
// delays emission and Temporal's retry policy handles the rest.
func (a *Activities) NotifyEmitter(ctx context.Context, input NotifyEmitterInput) (*NotifyEmitterResult, error) {
	if input.EmitterWebhookURL == "" {
		return nil, fmt.Errorf("notify emitter: EmitterWebhookURL is empty")
	}

	logger := activity.GetLogger(ctx)
	logger.Info("Notifying emitter", "url", input.EmitterWebhookURL, "snapshotID", input.SnapshotID)

	url := strings.TrimRight(input.EmitterWebhookURL, "/") + "/trigger-act"
	// Stage 3 only runs after CreateSnapshot has populated SnapshotID, so
	// we always send the field. No omitempty: the contract is "the
	// detector tells the emitter exactly which snapshot to read", with
	// no implicit "latest" fallback.
	payload, err := json.Marshal(struct {
		SnapshotID string `json:"snapshot_id"`
	}{SnapshotID: input.SnapshotID})
	if err != nil {
		// json.Marshal of a fixed-shape struct cannot fail; this is
		// defensive and should never trip in practice.
		return nil, fmt.Errorf("notify emitter: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("notify emitter: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	doer := a.HTTPDoer
	if doer == nil {
		doer = &http.Client{Timeout: 10 * time.Second}
	}

	resp, err := doer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("notify emitter: POST %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		// Read failures on a successful HTTP status are unusual but
		// possible (e.g. truncated response). Log and treat as empty
		// body — the status code below still drives success/failure.
		logger.Warn("Failed to read emitter response body", "error", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("notify emitter: %s returned status %d: %s",
			url, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out struct {
		WorkflowID string `json:"workflow_id"`
		RunID      string `json:"run_id"`
		SnapshotID string `json:"snapshot_id"`
	}
	if len(body) > 0 {
		if jsonErr := json.Unmarshal(body, &out); jsonErr != nil {
			// Successful status but unparseable body — emitter is
			// happy, our reply contract drifted. Don't fail the
			// workflow; just log and return what we have.
			logger.Warn("Emitter responded with unparseable body", "error", jsonErr, "body", string(body))
		}
	}

	logger.Info("Emitter notified",
		"workflowID", out.WorkflowID,
		"runID", out.RunID,
		"snapshotID", out.SnapshotID)

	return &NotifyEmitterResult{
		WorkflowID: out.WorkflowID,
		RunID:      out.RunID,
		SnapshotID: out.SnapshotID,
	}, nil
}
