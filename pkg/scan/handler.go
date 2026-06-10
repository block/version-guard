package scan

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"go.temporal.io/api/serviceerror"

	"github.com/block/Version-Guard/pkg/telemetry"
	"github.com/block/Version-Guard/pkg/types"
)

// Handler is an http.Handler that triggers a scan on POST.
// It is a thin transport adapter over Trigger.
type Handler struct {
	trigger *Trigger
}

// NewHandler returns an http.Handler that triggers scans via the given Trigger.
func NewHandler(t *Trigger) *Handler {
	return &Handler{trigger: t}
}

// requestBody is the JSON payload accepted by the handler.
// All fields are optional: an empty body triggers a full fleet scan.
type requestBody struct {
	ScanID        string   `json:"scan_id,omitempty"`
	ResourceTypes []string `json:"resource_types,omitempty"`
}

// responseBody is the JSON payload returned on success.
// Field order and JSON tags mirror Result so conversion is lossless.
type responseBody struct {
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
	ScanID     string `json:"scan_id"`
}

// ServeHTTP accepts POST requests with an optional JSON body and starts a
// scan. On success it returns 202 Accepted with run identifiers.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "only POST is supported")
		return
	}

	var body requestBody
	if r.ContentLength > 0 {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
			return
		}
	}

	resourceTypes := make([]types.ResourceType, 0, len(body.ResourceTypes))
	for _, rt := range body.ResourceTypes {
		resourceTypes = append(resourceTypes, types.ResourceType(rt))
	}

	res, err := h.trigger.Run(r.Context(), Input{
		ScanID:        body.ScanID,
		ResourceTypes: resourceTypes,
		Source:        telemetry.ScanSourceHTTP,
	})
	if err != nil {
		if isScanAlreadyRunning(err) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, responseBody(res))
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Encode error after WriteHeader is unrecoverable; the client has already
	// received the status. Swallowing keeps the handler interface clean.
	_ = json.NewEncoder(w).Encode(body) //nolint:errcheck // response already committed
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func isScanAlreadyRunning(err error) bool {
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	return errors.As(err, &alreadyStarted)
}
