package agent

import (
	"encoding/json"
	"sort"
)

// compactSchemaBytes bounds the first-turn schema so that it never displaces
// evidence; the 64 KiB request limit is unchanged.
const compactSchemaBytes = 10 << 10

// compactSchema returns table→columns and column→enum stored values, or nil
// when even the column listing would exceed the budget. Enums are dropped
// first. Only names and stored enum codes are included (no data values).
func compactSchema(full SnapshotDescription, budget int) (map[string][]string, map[string][]string) {
	if len(full.Tables) == 0 {
		return nil, nil
	}
	columns := make(map[string][]string, len(full.Tables))
	for table, cols := range full.Tables {
		columns[table] = append([]string{}, cols...)
	}
	enums := map[string][]string{}
	for column, values := range full.Enums {
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		enums[column] = keys
	}
	size := func(v any) int { raw, _ := json.Marshal(v); return len(raw) }
	if size(columns) > budget {
		return nil, nil
	}
	if len(enums) == 0 || size(columns)+size(enums) > budget {
		return columns, nil
	}
	return columns, enums
}
