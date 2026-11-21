package agent

import (
	"encoding/json"
	"strings"
	"time"
)

// Select-stage privacy: the model's select_metrics arguments are parsed into
// whitelisted, typed parameters instead of being substring-matched as a whole.
// Enumerated metric ids and validated dates cannot carry a contact or name, so
// they are never rejected by the known-contact dictionary. Free-text values
// (owner, sources, keyword) are checked individually; an offending value is
// dropped and the metric still runs. Nothing here stores the matched text.

type metricParamFinding struct {
	Param string `json:"param"`
	Rule  string `json:"rule"`
}

func validDate(v string) bool {
	if !datePattern.MatchString(v) {
		return false
	}
	_, err := time.ParseInLocation("2006-01-02", v, time.UTC)
	return err == nil
}

// freeTextRule reports why a free-text parameter value may not be used.
func freeTextRule(snapshot *Snapshot, value string) string {
	if snapshot == nil || snapshot.redactor == nil {
		return "guard_unavailable"
	}
	r := snapshot.redactor
	probe := strings.TrimSpace(normalizedSensitiveText(value))
	if probe == "" {
		return ""
	}
	if _, ok := r.contacts[probe]; ok {
		return "known_contact"
	}
	if _, ok := r.names[probe]; ok && !r.employees[probe] {
		return "known_identity"
	}
	for _, credential := range r.credentials {
		if c := normalizedSensitiveText(credential); c != "" && strings.Contains(probe, c) {
			return "credential"
		}
	}
	if r.contactReplacer != nil && len([]rune(probe)) >= minContactChars && r.contactReplacer.Replace(probe) != probe {
		return "known_contact"
	}
	if safe, excluded := snapshot.Sanitize(probe); excluded != 0 || safe != probe {
		return "sanitization"
	}
	return ""
}

// guardMetricSelections validates and cleans model selections. Invalid
// structured values are dropped (recorded as findings), never fatal.
func guardMetricSelections(snapshot *Snapshot, knownSources map[string]bool, selections []MetricSelection) ([]MetricSelection, []metricParamFinding) {
	findings := []metricParamFinding{}
	out := []MetricSelection{}
	for _, sel := range selections {
		if findMetric(sel.MetricID) == nil {
			findings = append(findings, metricParamFinding{"metric_id", "not_in_catalog"})
			continue
		}
		p := sel.Params
		for _, d := range []struct {
			name string
			v    *string
		}{{"date_from", &p.DateFrom}, {"date_to", &p.DateTo}} {
			*d.v = strings.TrimSpace(*d.v)
			if *d.v != "" && !validDate(*d.v) {
				findings = append(findings, metricParamFinding{d.name, "invalid_format"})
				*d.v = ""
			}
		}
		if p.DueBefore = strings.TrimSpace(p.DueBefore); p.DueBefore != "" && !dueBeforePattern.MatchString(p.DueBefore) {
			findings = append(findings, metricParamFinding{"due_before", "invalid_format"})
			p.DueBefore = ""
		}
		if p.Customer = strings.TrimSpace(p.Customer); p.Customer != "" && !customerRefPattern.MatchString(p.Customer) {
			findings = append(findings, metricParamFinding{"customer", "invalid_format"})
			p.Customer = ""
		}
		clean := func(name string, values []string) []string {
			kept := []string{}
			for _, v := range values {
				if knownSources[strings.TrimSpace(v)] {
					kept = append(kept, v)
					continue
				}
				if rule := freeTextRule(snapshot, v); rule != "" {
					findings = append(findings, metricParamFinding{name, rule})
					continue
				}
				kept = append(kept, v)
			}
			return kept
		}
		p.Sources = clean("sources", p.Sources)
		p.ExcludeSources = clean("exclude_sources", p.ExcludeSources)
		if p.Owner != "" && !knownEmployee(snapshot, p.Owner) {
			if rule := freeTextRule(snapshot, p.Owner); rule != "" {
				findings = append(findings, metricParamFinding{"owner", rule})
				p.Owner = ""
			}
		}
		if p.Keyword != "" {
			if rule := freeTextRule(snapshot, p.Keyword); rule != "" {
				findings = append(findings, metricParamFinding{"keyword", rule})
				p.Keyword = ""
			}
		}
		sel.Params = p
		out = append(out, sel)
	}
	return out, findings
}

// readMetricToolCalls parses the select-stage reply. The free-text
// cannot_answer reason and plain content keep the full provider check.
func readMetricToolCalls(snapshot *Snapshot, reply ModelMessage, provenance providerProvenance) (selections []MetricSelection, refusal string, refused bool, findings []providerPrivacyFinding) {
	textOnly := ModelMessage{Role: reply.Role, Content: reply.Content}
	if f := providerPrivacyFindingsWithProvenance(snapshot, textOnly, provenance); len(f) > 0 {
		findings = append(findings, f...)
	} else {
		refusal = strings.TrimSpace(reply.Content)
	}
	for _, tc := range reply.ToolCalls {
		switch tc.Function.Name {
		case "select_metrics":
			var p struct {
				Selections []MetricSelection `json:"selections"`
			}
			if json.Unmarshal([]byte(tc.Function.Arguments), &p) == nil {
				selections = append(selections, p.Selections...)
			}
		case "cannot_answer":
			var p struct {
				Reason string `json:"reason"`
			}
			if json.Unmarshal([]byte(tc.Function.Arguments), &p) == nil {
				refused = true
				if f := providerPrivacyFindingsWithProvenance(snapshot, ModelMessage{Role: "assistant", Content: p.Reason}, provenance); len(f) > 0 {
					for i := range f {
						f[i].Field = "tool_arguments"
					}
					findings = append(findings, f...)
					refusal = ""
				} else {
					refusal = p.Reason
				}
			}
		}
	}
	return selections, refusal, refused, findings
}

func knownEmployee(snapshot *Snapshot, value string) bool {
	if snapshot == nil || snapshot.redactor == nil {
		return false
	}
	v := strings.TrimSpace(value)
	if snapshot.redactor.employees[v] {
		return true
	}
	_, ok := snapshot.redactor.employeeIdentifiers[v]
	return ok
}
