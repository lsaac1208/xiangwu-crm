package agent

import "strconv"

// RecordCoverage is emitted only for the server-owned Search projection.
// An arbitrary SQL result's columns, sources or row count cannot establish
// record lineage, even when it aliases columns to these same names.
type RecordCoverage struct {
	Scope               string         `json:"scope"`
	DistinctRecords     int            `json:"distinct_records"`
	DistinctLeads       int            `json:"distinct_leads"`
	RecordsByType       map[string]int `json:"records_by_type"`
	RecordsWithoutLead  int            `json:"records_without_lead"`
	CompleteQueryOutput bool           `json:"complete_query_output"`
}

func searchRecordCoverage(r *Result) *RecordCoverage {
	indexes := make(map[string]int, len(r.Columns))
	for i, name := range r.Columns {
		indexes[name] = i
	}
	for _, name := range []string{"record_type", "record_id", "lead_id"} {
		if _, ok := indexes[name]; !ok {
			return nil
		}
	}
	coverage := &RecordCoverage{Scope: "returned_rows", RecordsByType: map[string]int{}, CompleteQueryOutput: r.Offset == 0 && int64(len(r.Rows)) == r.TotalRows}
	seen, leads := map[string]bool{}, map[int64]bool{}
	for _, row := range r.Rows {
		typ, id := textAt(row, indexes, "record_type"), numberAt(row, indexes, "record_id")
		if typ == "" || id <= 0 {
			return nil
		}
		key := typ + ":" + strconv.FormatInt(id, 10)
		if seen[key] {
			continue
		}
		seen[key] = true
		coverage.DistinctRecords++
		coverage.RecordsByType[typ]++
		leadID := numberAt(row, indexes, "lead_id")
		if leadID > 0 {
			leads[leadID] = true
		} else {
			coverage.RecordsWithoutLead++
		}
	}
	coverage.DistinctLeads = len(leads)
	return coverage
}
