package agent

import (
	"encoding/json"
	"strings"
)

// AnswerValidationAuditRecord contains only whitelisted final-answer fields,
// after the complete provider turn has passed the privacy gate. Private native
// reasoning is deliberately not an input to this audit API.
type AnswerValidationAuditRecord struct {
	RunID              string `json:"run_id"`
	ModelCall          int    `json:"model_call"`
	Status             string `json:"status"`
	Arguments          string `json:"arguments"`
	ArgumentBytes      int    `json:"argument_bytes"`
	ArgumentsTruncated bool   `json:"arguments_truncated"`
	ProposalAvailable  bool   `json:"proposal_available"`
	ValidationError    string `json:"validation_error"`
	ErrorBytes         int    `json:"error_bytes"`
	ErrorTruncated     bool   `json:"error_truncated"`
}

// Call only inside the already privacy-approved tool branch. The callback is
// optional diagnostic code, has no ability to alter a response, and cannot
// turn its own failure into a business result.
func emitAnswerValidationAudit(cfg Config, snapshot *Snapshot, runID string, modelCall int, proposal *FinalizeRequest, validationErr error) {
	if cfg.AnswerValidationAudit == nil {
		return
	}
	record := AnswerValidationAuditRecord{RunID: runID, ModelCall: modelCall, Status: "accepted"}
	if validationErr != nil {
		record.Status = "rejected"
		record.ValidationError, _ = snapshot.Sanitize(validationErr.Error())
	}
	if proposal != nil {
		// Marshaling the typed request excludes unrecognized provider properties
		// such as reasoning_content, rather than copying arbitrary tool JSON.
		raw, err := json.Marshal(proposal)
		if err == nil {
			record.Arguments, record.ProposalAvailable = string(raw), true
		}
	}
	credentials := []string{cfg.APIKey}
	if cfg.Fallback != nil {
		credentials = append(credentials, cfg.Fallback.APIKey)
	}
	if snapshot != nil && snapshot.redactor != nil {
		credentials = append(credentials, snapshot.redactor.credentials...)
	}
	for _, key := range credentials {
		if key != "" {
			record.Arguments = strings.ReplaceAll(record.Arguments, key, "[REDACTED]")
			record.ValidationError = strings.ReplaceAll(record.ValidationError, key, "[REDACTED]")
		}
	}
	record.ArgumentBytes, record.ErrorBytes = len(record.Arguments), len(record.ValidationError)
	record.Arguments = boundedUTF8(record.Arguments, 16<<10)
	record.ValidationError = boundedUTF8(record.ValidationError, 4<<10)
	record.ArgumentsTruncated = len(record.Arguments) < record.ArgumentBytes
	record.ErrorTruncated = len(record.ValidationError) < record.ErrorBytes
	defer func() { _ = recover() }()
	cfg.AnswerValidationAudit(record)
}
