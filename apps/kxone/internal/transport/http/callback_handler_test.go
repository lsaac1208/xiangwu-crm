package http

import "testing"

func TestExtractLeadIDFromTaskID(t *testing.T) {
	tests := []struct {
		name   string
		taskID string
		want   int64
	}{
		{name: "legacy format", taskID: "lead:42:sales:sales1:claim", want: 42},
		{name: "short format", taskID: "l42_spc", want: 42},
		{name: "short resend format", taskID: "l42_spd_r6a", want: 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractLeadIDFromTaskID(tt.taskID)
			if err != nil {
				t.Fatalf("extractLeadIDFromTaskID() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("extractLeadIDFromTaskID() = %d, want %d", got, tt.want)
			}
		})
	}
}
