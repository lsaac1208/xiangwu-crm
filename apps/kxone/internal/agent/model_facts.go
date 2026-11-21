package agent

import "time"

// Model facts inherit unchanged identifiers from their result envelope. Full
// MeasuredFact objects remain in storage and are used for answer validation.
// Overrides preserve unusual/mismatched metadata rather than silently making
// an invalid stored fact look as if it belongs to the enclosing result.
type modelFact struct {
	ID          string     `json:"fact_id"`
	ResultID    *string    `json:"result_id,omitempty"`
	ScopeID     *string    `json:"scope_id,omitempty"`
	SnapshotAt  *time.Time `json:"snapshot_at,omitempty"`
	Level       string     `json:"level"`
	Metric      string     `json:"metric"`
	RecordTypes []string   `json:"record_types,omitempty"`
	Group       string     `json:"group,omitempty"`
	GroupLabel  string     `json:"group_label,omitempty"`
	Value       int64      `json:"value"`
	Additive    bool       `json:"additive"`
	Unit        string     `json:"unit"`
}

const modelFactInstruction = "facts继承所属结果的id、snapshot_at和measure_scope.id；事实自身同名字段只在不同时覆盖。level区分global/group，group为原分组键；additive=false不可相加。"

func compactModelFact(f MeasuredFact, r *Result) modelFact {
	out := modelFact{ID: f.ID, Level: f.Level, Metric: f.Metric, RecordTypes: f.RecordTypes, Group: f.Group, GroupLabel: f.GroupLabel, Value: f.Value, Additive: f.Additive, Unit: "条"}
	if f.ResultID != r.ID {
		id := f.ResultID
		out.ResultID = &id
	}
	if r.MeasureScope == nil || f.ScopeID != r.MeasureScope.ID {
		id := f.ScopeID
		out.ScopeID = &id
	}
	if !f.SnapshotAt.Equal(r.SnapshotAt) {
		at := f.SnapshotAt
		out.SnapshotAt = &at
	}
	return out
}
