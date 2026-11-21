package agent

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// boundNumbers are values the server can prove from the answer's own
// references: the value of a referenced, actually-read cell, or the number of
// distinct referenced rows of one result. A prose literal equal to one of them
// (with a compatible unit) is treated as bound instead of rejected.
// v27/v28 A13: "客户#2 有 2 条历史回访计划 … current_followup_eligible 为 0"
// referenced exactly those two rows and that cell, yet was rejected.
type boundNumbers struct {
	cells     []boundCell
	rowCounts map[float64]bool
}

type boundCell struct {
	value  float64
	column string
}

var countUnits = "条个项次笔份户组位名件单批家人"

func collectBoundNumbers(refs []AnswerReference, results map[string]*Result, readViews ...*evidenceReadState) *boundNumbers {
	b := &boundNumbers{rowCounts: map[float64]bool{}}
	rows := map[string]map[int]bool{}
	for _, ref := range refs {
		if ref.FactID != "" || ref.Row == nil || ref.ResultID == "" || ref.Column == "" {
			continue
		}
		r := results[ref.ResultID]
		if r == nil {
			continue
		}
		var value any
		found := false
		if len(readViews) > 0 && readViews[0] != nil {
			if row, ok := readViews[0].values[ref.ResultID][*ref.Row]; ok {
				value, found = row[ref.Column]
			}
		} else {
			local := *ref.Row - r.Offset
			for i, c := range r.Columns {
				if c == ref.Column && local >= 0 && local < len(r.Rows) && i < len(r.Rows[local]) {
					value, found = r.Rows[local][i], true
				}
			}
		}
		if !found {
			continue // unread cells never bind anything
		}
		if rows[ref.ResultID] == nil {
			rows[ref.ResultID] = map[int]bool{}
		}
		rows[ref.ResultID][*ref.Row] = true
		if f, ok := numericValue(value); ok {
			b.cells = append(b.cells, boundCell{value: f, column: strings.ToLower(ref.Column)})
		}
	}
	for resultID, set := range rows {
		b.rowCounts[float64(len(set))] = true
		// Per-lead counts within one result (v30 A21-2: “客户#1两条…客户#2一条”
		// over three referenced follow-up rows carrying lead_id).
		perLead := map[string]int{}
		for row := range set {
			if lead, ok := readCellValue(resultID, row, "lead_id", results, readViews...); ok {
				perLead[textValue(lead)]++
			}
		}
		for _, n := range perLead {
			b.rowCounts[float64(n)] = true
		}
	}
	return b
}

func numericValue(v any) (float64, bool) {
	switch t := v.(type) {
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	case interface{ String() string }:
		f, err := strconv.ParseFloat(t.String(), 64)
		return f, err == nil
	}
	return 0, false
}

var chineseDigits = map[rune]float64{'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}

// parseLiteral returns the numeric value and the unit rune following it.
func parseLiteral(literal, after string) (float64, rune, bool) {
	literal = strings.TrimSpace(literal)
	if literal == "" {
		return 0, 0, false
	}
	if f, err := strconv.ParseFloat(literal, 64); err == nil {
		unit, _ := utf8.DecodeRuneInString(strings.TrimLeftFunc(after, unicode.IsSpace))
		return f, unit, true
	}
	// Chinese quantity such as 两条 / 十二个: the regexp includes the unit.
	runes := []rune(strings.ReplaceAll(literal, " ", ""))
	unit := runes[len(runes)-1]
	digits := runes[:len(runes)-1]
	total, current := 0.0, 0.0
	for _, r := range digits {
		if d, ok := chineseDigits[r]; ok {
			current = d
			continue
		}
		switch r {
		case '十':
			if current == 0 {
				current = 1
			}
			total += current * 10
			current = 0
		default:
			return 0, 0, false // 百/千/万 etc.: do not guess
		}
	}
	return total + current, unit, len(digits) > 0
}

func (b *boundNumbers) allows(value float64, unit rune) bool {
	if b == nil {
		return false
	}
	isCount := unit == 0 || strings.ContainsRune(countUnits, unit) || !(unit == '元' || unit == '%' || unit == '％' || unit == '分' || unit == '秒' || unit == '天' || unit == '小' || unit == '成' || unicode.IsLetter(unit) && unit < 128)
	if b.rowCounts[value] && (unit == 0 || strings.ContainsRune(countUnits, unit)) {
		return true
	}
	for _, c := range b.cells {
		if math.Abs(c.value-value) > 1e-9 {
			continue
		}
		switch {
		case unit == '元':
			if strings.Contains(c.column, "yuan") || strings.Contains(c.column, "元") || strings.Contains(c.column, "cny") {
				return true
			}
		case unit == '%' || unit == '％':
			if strings.Contains(c.column, "percent") || strings.Contains(c.column, "pct") || strings.Contains(c.column, "率") || strings.Contains(c.column, "rate") {
				return true
			}
		case isCount:
			return true
		}
	}
	return false
}

// unboundNumberLiterals lists number literals in plain text that are not
// proven by bound. With bound == nil every literal is unbound (old behaviour).
func unboundNumberLiterals(plain string, bound *boundNumbers) []string {
	out := []string{}
	for _, loc := range answerNumber.FindAllStringIndex(plain, -1) {
		literal := plain[loc[0]:loc[1]]
		value, unit, ok := parseLiteral(literal, plain[loc[1]:])
		if ok && bound.allows(value, unit) {
			continue
		}
		out = append(out, literal)
	}
	return out
}

func readCellValue(resultID string, row int, column string, results map[string]*Result, readViews ...*evidenceReadState) (any, bool) {
	if len(readViews) > 0 && readViews[0] != nil {
		values, ok := readViews[0].values[resultID][row]
		if !ok {
			return nil, false
		}
		v, ok := values[column]
		return v, ok
	}
	r := results[resultID]
	if r == nil {
		return nil, false
	}
	local := row - r.Offset
	for i, c := range r.Columns {
		if c == column && local >= 0 && local < len(r.Rows) && i < len(r.Rows[local]) {
			return r.Rows[local][i], true
		}
	}
	return nil, false
}

// referencedResultIDs lists, in first-use order, the real results that the
// references bind (cells directly, facts through their owning result).
func referencedResultIDs(refs []AnswerReference, results map[string]*Result) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(id string) {
		if id != "" && results[id] != nil && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, ref := range refs {
		if ref.FactID == "" {
			add(ref.ResultID)
			continue
		}
		for id, r := range results {
			for _, f := range r.Facts {
				if f.ID == ref.FactID {
					add(id)
				}
			}
		}
	}
	return out
}
