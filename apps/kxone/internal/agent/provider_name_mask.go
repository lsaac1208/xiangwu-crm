package agent

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// providerNameHint is privacy-safe diagnosis of an unknown_name finding: the
// single surname character that started the heuristic match, which rule
// matched, and the coarse category of the neighbouring characters. No other
// source text is retained.
type providerNameHint struct {
	Rule    string `json:"rule"`
	Surname string `json:"surname"`
	Before  string `json:"before"`
	After   string `json:"after"`
}

func runeCategory(r rune, ok bool) string {
	switch {
	case !ok:
		return "boundary"
	case unicode.Is(unicode.Han, r):
		return "han"
	case unicode.IsDigit(r):
		return "digit"
	case unicode.IsLetter(r):
		return "latin"
	case unicode.IsSpace(r):
		return "space"
	default:
		return "punct"
	}
}

func nameHints(r *redactor, probe string) []providerNameHint {
	if r == nil || r.trustedDimension {
		return nil
	}
	text := degreeSurnamePattern.ReplaceAllStringFunc(probe, shieldDegreeSurname)
	hints := []providerNameHint{}
	add := func(rule string, start, end, surnameAt int) {
		if len(hints) >= 8 || surnameAt < 0 || surnameAt >= len(text) {
			return
		}
		surname, _ := utf8.DecodeRuneInString(text[surnameAt:])
		if !strings.ContainsRune(surnameChars, surname) {
			return
		}
		before, sizeBefore := utf8.DecodeLastRuneInString(text[:start])
		after, sizeAfter := utf8.DecodeRuneInString(text[end:])
		hints = append(hints, providerNameHint{Rule: rule, Surname: string(surname), Before: runeCategory(before, sizeBefore > 0), After: runeCategory(after, sizeAfter > 0)})
	}
	for _, loc := range proseNamePattern.FindAllStringIndex(text, -1) {
		add("prose", loc[0], loc[1], loc[0])
	}
	for _, loc := range familyTitlePattern.FindAllStringIndex(text, -1) {
		add("family_title", loc[0], loc[1], loc[0])
	}
	for _, loc := range standaloneNamePattern.FindAllStringSubmatchIndex(text, -1) {
		add("standalone", loc[0], loc[1], loc[2])
	}
	return hints
}

// Tools whose arguments are presentation prose. A suspected unknown name in
// them can be masked without changing any query or evidence binding.
var maskableNameTools = map[string]bool{"finalize_answer": true, "accept_answer_repair": true, "present_result": true, "select_final_results": true}

// Keys that carry identifiers or schema names, never masked.
var unmaskedArgumentKeys = map[string]bool{"result_id": true, "result_ids": true, "table_result_ids": true, "fact_id": true, "proposal_id": true, "token": true, "column": true, "source_message_id": true, "type": true, "chart_type": true, "x_column": true, "y_columns": true, "kind": true, "level": true, "comparison": true, "row": true}

// maskUnknownNames returns a copy of a provider turn with heuristic
// unknown-name fragments masked, when that is the only finding category, the
// turn has no native reasoning, and only presentation prose changes. Known
// customer identities, contacts and credentials are never masked: those
// findings keep rejecting the whole turn.
func maskUnknownNames(snapshot *Snapshot, message ModelMessage, findings []providerPrivacyFinding) (ModelMessage, bool) {
	if snapshot == nil || snapshot.redactor == nil || len(findings) == 0 {
		return message, false
	}
	for _, f := range findings {
		if f.Rule != "unknown_name" || f.Field == "reasoning" {
			return message, false
		}
	}
	if message.ReasoningContent != nil && strings.TrimSpace(*message.ReasoningContent) != "" {
		return message, false
	}
	masked := message
	masked.ReasoningContent = message.ReasoningContent
	if safe, _ := snapshot.Sanitize(message.Content); safe != normalizedSensitiveText(message.Content) {
		masked.Content = safe
	}
	masked.ToolCalls = append([]ToolCall{}, message.ToolCalls...)
	for i, call := range masked.ToolCalls {
		var payload any
		decoder := json.NewDecoder(strings.NewReader(call.Function.Arguments))
		decoder.UseNumber()
		if decoder.Decode(&payload) != nil {
			return message, false
		}
		changed := false
		var walk func(v any, key string) any
		walk = func(v any, key string) any {
			switch t := v.(type) {
			case map[string]any:
				for k, item := range t {
					t[k] = walk(item, k)
				}
				return t
			case []any:
				for j, item := range t {
					t[j] = walk(item, key)
				}
				return t
			case string:
				if unmaskedArgumentKeys[key] {
					return t
				}
				safe, _ := snapshot.Sanitize(t)
				if safe != normalizedSensitiveText(t) {
					changed = true
					return safe
				}
				return t
			}
			return v
		}
		payload = walk(payload, "")
		if !changed {
			continue
		}
		if !maskableNameTools[call.Function.Name] {
			return message, false
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return message, false
		}
		masked.ToolCalls[i].Function.Arguments = string(raw)
	}
	return masked, true
}
