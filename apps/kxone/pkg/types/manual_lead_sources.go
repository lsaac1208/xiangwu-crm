package types

import (
	_ "embed"
	"encoding/json"
)

// Shared with the CRM source dropdown so API validation and UI stay aligned.
//
//go:embed manual_lead_sources.json
var manualLeadSourcesJSON []byte

var manualLeadSources = func() []string {
	var sources []string
	if err := json.Unmarshal(manualLeadSourcesJSON, &sources); err != nil {
		panic(err)
	}
	return sources
}()

func IsManualLeadSource(source string) bool {
	for _, candidate := range manualLeadSources {
		if candidate == source {
			return true
		}
	}
	return false
}
