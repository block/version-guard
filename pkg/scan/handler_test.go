package scan

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/Version-Guard/pkg/types"
	"github.com/block/Version-Guard/pkg/workflow/orchestrator"
)

func newTestHandler(t *testing.T, mock *mockStarter) *Handler {
	t.Helper()
	// Provide a default so empty-body POSTs (full-fleet scan) succeed —
	// the orchestrator workflow now rejects empty ResourceTypes.
	defaults := []types.ResourceType{"aurora-mysql"}
	return NewHandler(NewTriggerWithStarter(mock, "version-guard-orchestrator", defaults))
}

func TestHandler_POST_EmptyBody_TriggersFullScan(t *testing.T) {
	mock := &mockStarter{run: &mockWorkflowRun{id: "wf", runID: "run"}}
	h := newTestHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/scan", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	require.True(t, mock.called)

	var body responseBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "wf", body.WorkflowID)
	assert.Equal(t, "run", body.RunID)
	assert.NotEmpty(t, body.ScanID)

	require.Len(t, mock.calledArgs, 1)
	in, ok := mock.calledArgs[0].(orchestrator.WorkflowInput)
	require.True(t, ok, "workflow args[0] should be orchestrator.WorkflowInput")
	assert.Equal(t, orchestrator.ScanScopeFull, in.ScanScope)
}

func TestHandler_POST_TargetedScan(t *testing.T) {
	mock := &mockStarter{run: &mockWorkflowRun{id: "wf", runID: "run"}}
	h := newTestHandler(t, mock)

	reqBody := `{"resource_types":["aurora-mysql","eks"],"scan_id":"my-scan"}`
	req := httptest.NewRequest(http.MethodPost, "/scan", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	var body responseBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "my-scan", body.ScanID)

	// Inspect that the trigger received the right ResourceTypes
	require.Len(t, mock.calledArgs, 1)
	in, ok := mock.calledArgs[0].(orchestrator.WorkflowInput)
	require.True(t, ok, "workflow args[0] should be orchestrator.WorkflowInput")
	assert.Equal(t, []types.ResourceType{"aurora-mysql", "eks"}, in.ResourceTypes)
	assert.Equal(t, "my-scan", in.ScanID)
	assert.Equal(t, orchestrator.ScanScopeTargeted, in.ScanScope)
}

func TestHandler_RejectsNonPOST(t *testing.T) {
	mock := &mockStarter{}
	h := newTestHandler(t, mock)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/scan", http.NoBody)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, method)
		assert.Equal(t, http.MethodPost, rec.Header().Get("Allow"))
	}
	assert.False(t, mock.called, "trigger should never run for non-POST")
}

func TestHandler_InvalidJSON_Returns400(t *testing.T) {
	mock := &mockStarter{}
	h := newTestHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/scan", strings.NewReader("not json"))
	req.ContentLength = int64(len("not json"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.False(t, mock.called)
}

func TestHandler_UnknownFields_Returns400(t *testing.T) {
	mock := &mockStarter{}
	h := newTestHandler(t, mock)

	reqBody := `{"unexpected":"field"}`
	req := httptest.NewRequest(http.MethodPost, "/scan", strings.NewReader(reqBody))
	req.ContentLength = int64(len(reqBody))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_TriggerError_Returns500(t *testing.T) {
	mock := &mockStarter{err: errors.New("temporal unavailable")}
	h := newTestHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/scan", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "temporal unavailable")
}
