package agent

import (
	"encoding/json"
	"sort"
	"time"
)

type checkpointRow struct {
	Row   int     `json:"row"`
	Cells [][]any `json:"cells"` // [zero-based column index, fully observed value]
}

type checkpointResult struct {
	ID              string          `json:"id"`
	SnapshotAt      time.Time       `json:"snapshot_at"`
	MeasureScope    *MeasureScope   `json:"measure_scope,omitempty"`
	Columns         []string        `json:"columns"`
	Rows            []checkpointRow `json:"rows"`
	Facts           []modelFact     `json:"facts"`
	OmittedCells    int             `json:"omitted_cells"`
	OmittedFacts    int             `json:"omitted_facts"`
	RecoveryFromRow int             `json:"recovery_from_row"`
}

type observedCell struct {
	row    int
	column string
	value  any
}

type checkpointCandidate struct {
	result     *Result
	page       *checkpointResult
	facts      []MeasuredFact
	cells      []observedCell
	nextFact   int
	nextCell   int
	columnKeys map[string]int
	rowKeys    map[int]int
}

// Preserve only delivered evidence. Pack values by result and column instead
// of repeating a UUID and column name per cell. Take turns among results so a
// long detail table cannot evict a small aggregate solely because its random
// UUID sorts first. This changes neither read authorization nor stored results.
func (s *evidenceReadState) checkpoint(results map[string]*Result, budget int, selected ...[]string) map[string]any {
	ids := make([]string, 0, len(results))
	for id := range results {
		ids = append(ids, id)
	}
	priority := map[string]bool{}
	if len(selected) > 0 {
		for _, id := range selected[0] {
			priority[id] = true
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if priority[ids[i]] != priority[ids[j]] {
			return priority[ids[i]]
		}
		return ids[i] < ids[j]
	})
	pages := []*checkpointResult{}
	out := map[string]any{"results": pages, "omitted_cells": 0, "omitted_facts": 0, "omitted_result_ids": []string{}, "instruction": "仅包含同一运行已实际完整读取的事实和单元格，可直接引用。每结果columns为本段列目录；rows[].row是原结果绝对行号，cells每项为[columns索引,原值]，未列出的单元格不能视为已读。omitted计数仅指已读但未带入的内容；有遗漏时read_result(id,offset=recovery_from_row)按页恢复，-1表示无遗漏。" + modelFactInstruction}
	fits := func() bool { raw, _ := json.Marshal(out); return len(raw)+64 <= budget }
	var candidates []*checkpointCandidate
	totalCells, totalFacts := 0, 0
	for _, id := range ids {
		r := results[id]
		c := &checkpointCandidate{result: r, page: &checkpointResult{ID: id, SnapshotAt: r.SnapshotAt, MeasureScope: r.MeasureScope, Columns: []string{}, Rows: []checkpointRow{}, Facts: []modelFact{}, RecoveryFromRow: -1}, columnKeys: map[string]int{}, rowKeys: map[int]int{}}
		for _, f := range r.Facts {
			if s.facts[f.ID] {
				c.facts = append(c.facts, f)
			}
		}
		rows := make([]int, 0, len(s.values[id]))
		for row := range s.values[id] {
			rows = append(rows, row)
		}
		sort.Ints(rows)
		for _, row := range rows {
			columns := make([]string, 0, len(s.values[id][row]))
			for col := range s.values[id][row] {
				columns = append(columns, col)
			}
			sort.Strings(columns)
			for _, col := range columns {
				c.cells = append(c.cells, observedCell{row: row, column: col, value: s.values[id][row][col]})
			}
		}
		c.page.OmittedCells, c.page.OmittedFacts = len(c.cells), len(c.facts)
		totalCells += len(c.cells)
		totalFacts += len(c.facts)
		if len(c.cells)+len(c.facts) == 0 && !s.results[id] {
			continue
		}
		pages = append(pages, c.page)
		out["results"] = pages
		if !fits() {
			pages = pages[:len(pages)-1]
			out["results"] = pages
			out["omitted_result_ids"] = append(out["omitted_result_ids"].([]string), id)
			continue
		}
		candidates = append(candidates, c)
	}
	carriedCells, carriedFacts := 0, 0
	for {
		remaining := false
		for _, c := range candidates {
			if c.nextFact < len(c.facts) {
				remaining = true
				f := c.facts[c.nextFact]
				c.nextFact++
				c.page.Facts = append(c.page.Facts, compactModelFact(f, c.result))
				if fits() {
					c.page.OmittedFacts--
					carriedFacts++
				} else {
					c.page.Facts = c.page.Facts[:len(c.page.Facts)-1]
					c.page.RecoveryFromRow = 0
				}
			}
			if c.nextCell < len(c.cells) {
				// Carry one complete observed row per turn, so each result keeps
				// whole rows instead of a column-ordered prefix of cells.
				remaining = true
				start := c.nextCell
				end := start
				for end < len(c.cells) && c.cells[end].row == c.cells[start].row {
					end++
				}
				c.nextCell = end
				rowNumber := c.cells[start].row
				savedColumns := len(c.page.Columns)
				savedRows := len(c.page.Rows)
				addedColumns := map[string]int{}
				rowIndex := len(c.page.Rows)
				c.page.Rows = append(c.page.Rows, checkpointRow{Row: rowNumber, Cells: [][]any{}})
				for _, cell := range c.cells[start:end] {
					column, known := c.columnKeys[cell.column]
					if !known {
						if added, ok := addedColumns[cell.column]; ok {
							column = added
						} else {
							column = len(c.page.Columns)
							c.page.Columns = append(c.page.Columns, cell.column)
							addedColumns[cell.column] = column
						}
					}
					c.page.Rows[rowIndex].Cells = append(c.page.Rows[rowIndex].Cells, []any{column, cell.value})
				}
				if fits() {
					for name, index := range addedColumns {
						c.columnKeys[name] = index
					}
					c.rowKeys[rowNumber] = rowIndex
					c.page.OmittedCells -= end - start
					carriedCells += end - start
				} else {
					c.page.Rows = c.page.Rows[:savedRows]
					c.page.Columns = c.page.Columns[:savedColumns]
					if c.page.RecoveryFromRow < 0 || rowNumber < c.page.RecoveryFromRow {
						c.page.RecoveryFromRow = rowNumber
					}
				}
			}
		}
		if !remaining {
			break
		}
	}
	out["omitted_cells"], out["omitted_facts"] = totalCells-carriedCells, totalFacts-carriedFacts
	return out
}
