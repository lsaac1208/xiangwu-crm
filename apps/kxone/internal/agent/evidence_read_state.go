package agent

import (
	"errors"
)

// The model may reference only cells/facts delivered in a tool page, not merely
// materialized for export. Retained results are still immutable full evidence.
type evidenceReadState struct {
	dictionary *dictionaryReadState
	results    map[string]bool
	cells      map[string]map[int]map[string]bool
	facts      map[string]bool
	values     map[string]map[int]map[string]any
}

func newEvidenceReadState() *evidenceReadState {
	return &evidenceReadState{results: map[string]bool{}, cells: map[string]map[int]map[string]bool{}, facts: map[string]bool{}, values: map[string]map[int]map[string]any{}}
}
func (s *evidenceReadState) observe(output any) {
	page, ok := output.(map[string]any)
	if !ok {
		return
	}
	id, _ := page["id"].(string)
	if id == "" {
		return
	}
	s.results[id] = true
	if facts, ok := page["facts"].([]MeasuredFact); ok {
		for _, f := range facts {
			s.facts[f.ID] = true
		}
	}
	if facts, ok := page["facts"].([]modelFact); ok {
		for _, f := range facts {
			s.facts[f.ID] = true
		}
	}
	rows, ok := page["rows"].([][]any)
	if !ok {
		return
	}
	cols, _ := page["columns"].([]string)
	offset, _ := page["offset"].(int)
	if s.cells[id] == nil {
		s.cells[id] = map[int]map[string]bool{}
		s.values[id] = map[int]map[string]any{}
	}
	truncated := map[int]map[string]bool{}
	if segments, ok := page["text_segments"].([]map[string]any); ok {
		for _, seg := range segments {
			row, _ := seg["row"].(int)
			col, _ := seg["column"].(string)
			if truncated[row] == nil {
				truncated[row] = map[string]bool{}
			}
			truncated[row][col] = true
		}
	}
	for i, row := range rows {
		at := offset + i
		if s.cells[id][at] == nil {
			s.cells[id][at] = map[string]bool{}
			s.values[id][at] = map[string]any{}
		}
		for c := range row {
			if c < len(cols) && !truncated[at][cols[c]] {
				s.cells[id][at][cols[c]] = true
				s.values[id][at][cols[c]] = row[c]
			}
		}
	}
}
func (s *evidenceReadState) validate(refs []AnswerReference) error {
	for _, ref := range refs {
		if (ref.Level == "global" || ref.Level == "group") && ref.FactID == "" {
			return errors.New("全局/分组引用缺少fact_id，请先measure_record_set取得真实事实，不能用null或单元格代替")
		}
		if ref.FactID != "" {
			if !s.facts[ref.FactID] {
				return errors.New("事实尚未通过工具页读取，请先read_result")
			}
		} else if ref.Row == nil || !s.cells[ref.ResultID][*ref.Row][ref.Column] {
			return errors.New("引用单元格尚未读取，请先read_result对应页")
		}
	}
	return nil
}
