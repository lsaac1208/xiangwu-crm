package agent

import (
	"encoding/json"
	"testing"
)

// Strict providers validate every function on every request. A nil Go slice
// must never turn an optional required list into JSON null (invalid schema).
func TestAgentToolSchemasUseRequiredArrays(t *testing.T) {
	raw, err := json.Marshal(agentTools())
	if err != nil {
		t.Fatal(err)
	}
	var tools []struct {
		Function struct {
			Name       string `json:"name"`
			Parameters struct {
				Required   any            `json:"required"`
				Properties map[string]any `json:"properties"`
			} `json:"parameters"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		required, ok := tool.Function.Parameters.Required.([]any)
		if !ok {
			t.Fatalf("function %s has a non-array required list", tool.Function.Name)
		}
		for _, field := range required {
			name, ok := field.(string)
			if !ok || tool.Function.Parameters.Properties[name] == nil {
				t.Fatalf("function %s requires an undefined property", tool.Function.Name)
			}
		}
	}
}
