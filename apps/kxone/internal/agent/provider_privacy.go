package agent

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var providerIdentifierToken = regexp.MustCompile(`[A-Za-z0-9_-]+`)
var providerDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Constructed only from server records, never from a provider response or a
// user-supplied identifier list. It is local to one authorized execution.
type providerProvenance map[string]bool

func (p providerProvenance) addUUID(id string) {
	parsed, err := uuid.Parse(id)
	if err == nil && parsed.Version() == 4 && parsed.Variant() == uuid.RFC4122 && parsed.String() == id {
		p[id] = true
	}
}

func (s *Service) loadProviderProvenance(ctx context.Context, run Run, history []Message, conditions AnalysisContext) (providerProvenance, error) {
	if run.AccountID <= 0 || len(history) > 12 || len(conditions.Conditions) > 20 {
		return nil, errors.New("分析内部标识来源无法确认")
	}
	known := providerProvenance{}
	known.addUUID(run.ID)
	known.addUUID(run.ConversationID)
	if !known[run.ID] || !known[run.ConversationID] {
		return nil, errors.New("分析内部标识来源无法确认")
	}
	// Only messages already in the model context, or explicitly retained as a
	// condition source, need lookup. Old conditions remain bounded independently
	// from recent conversation history. Do not fetch message/customer text here.
	recent := map[string]bool{}
	wanted := map[string]bool{}
	for _, m := range history {
		if m.ConversationID == run.ConversationID {
			recent[m.ID] = true
			wanted[m.ID] = true
		}
	}
	for _, c := range conditions.Conditions {
		wanted[c.SourceMessageID] = true
	}
	ids := make([]string, 0, len(wanted))
	for id := range wanted {
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return known, nil
	}
	var sources []Message
	err := s.db.WithContext(ctx).Table("ai_messages AS m").Select("m.id,m.conversation_id,m.run_id,m.role").
		Joins("JOIN ai_runs AS r ON r.id=m.run_id AND r.conversation_id=m.conversation_id").
		Where("m.conversation_id=? AND r.account_id=? AND m.id IN ?", run.ConversationID, run.AccountID, ids).
		Scan(&sources).Error
	if err != nil {
		return nil, errors.New("读取分析内部标识来源失败")
	}
	for _, m := range sources {
		// Persisted condition sources must actually be user messages. The
		// ordinary final-condition gate still checks their original text.
		if m.Role != "user" && (!recent[m.ID] || m.Role != "assistant") {
			continue
		}
		known.addUUID(m.ID)
		known.addUUID(m.RunID)
	}
	return known, nil
}

// Called only after read_history returned a successful, authorized text page.
// Recheck its metadata relationship before granting new identifier provenance;
// neither an unexecuted model parameter nor a failed/foreign read can add IDs.
func (s *Service) extendProviderProvenance(ctx context.Context, known providerProvenance, run Run, message Message) error {
	if known == nil || message.ConversationID != run.ConversationID {
		return errors.New("历史消息标识来源无法确认")
	}
	verified, err := s.loadProviderProvenance(ctx, run, []Message{message}, AnalysisContext{})
	if err != nil {
		return err
	}
	if !verified[message.ID] || !verified[message.RunID] {
		return errors.New("历史消息标识来源无法确认")
	}
	for id := range verified {
		known[id] = true
	}
	return nil
}

// These diagnostics deliberately contain no source text, matched value, tool
// name, or provider reasoning. Field and rule values are local fixed enums.
type providerPrivacyFinding struct {
	Field string `json:"field"`
	Rule  string `json:"rule"`
	Count int    `json:"count"`
	Bytes int    `json:"bytes"`
	// Only for unknown_name: surname character and neighbour categories.
	Hints []providerNameHint `json:"name_hints,omitempty"`
}

// Accept or reject the whole native response. Editing a tool argument or a
// reasoning field would change its meaning/protocol; unchecked content must
// never be replayed, persisted as an answer or published in a stream.
func providerTurnSafe(snapshot *Snapshot, message ModelMessage, savedResults ...map[string]*Result) bool {
	return len(providerPrivacyFindings(snapshot, message, savedResults...)) == 0
}

func providerPrivacyFindings(snapshot *Snapshot, message ModelMessage, savedResults ...map[string]*Result) []providerPrivacyFinding {
	return providerPrivacyFindingsWithProvenance(snapshot, message, nil, savedResults...)
}

func providerPrivacyFindingsWithProvenance(snapshot *Snapshot, message ModelMessage, provenance providerProvenance, savedResults ...map[string]*Result) []providerPrivacyFinding {
	if snapshot == nil || snapshot.redactor == nil {
		return []providerPrivacyFinding{{Field: "content", Rule: "guard_unavailable", Count: 1}}
	}
	known := providerProvenance{}
	for id := range provenance {
		known.addUUID(id)
	}
	for _, results := range savedResults {
		for id, result := range results {
			if result == nil || result.ID != id || !result.SnapshotAt.Equal(snapshot.At) {
				continue
			}
			known.addUUID(id)
			if result.MeasureScope != nil && known[id] {
				for _, digest := range []string{result.MeasureScope.ID, result.MeasureScope.SelectionHash} {
					if providerDigest.MatchString(digest) {
						known[digest] = true
					}
				}
			}
			for _, fact := range result.Facts {
				if result.MeasureScope != nil && fact.ResultID == id && fact.ScopeID == result.MeasureScope.ID && fact.SnapshotAt.Equal(snapshot.At) {
					known.addUUID(fact.ID)
				}
			}
		}
	}
	type fieldValue struct{ field, value string }
	values := []fieldValue{{"content", message.Content}}
	if message.ReasoningContent != nil {
		values = append(values, fieldValue{"reasoning", *message.ReasoningContent})
	}
	for _, call := range message.ToolCalls {
		values = append(values, fieldValue{"tool_arguments", call.Function.Arguments})
	}
	var findings []providerPrivacyFinding
	for _, value := range values {
		probe := normalizedSensitiveText(value.value)
		addFinding := func(rule string, count int) {
			findings = append(findings, providerPrivacyFinding{Field: value.field, Rule: rule, Count: count, Bytes: len(value.value)})
		}
		knownPrivate := false
		// A legitimate generated identifier must never conceal an actual known
		// customer/contact or either provider's credential. Check those exact
		// dictionaries before shielding the generic phone-shaped UUID digits.
		for _, credential := range snapshot.redactor.credentials {
			credential = normalizedSensitiveText(credential)
			if credential != "" && strings.Contains(probe, credential) {
				addFinding("credential", strings.Count(probe, credential))
				knownPrivate = true
			}
		}
		if snapshot.redactor.nameReplacer != nil && snapshot.redactor.nameReplacer.Replace(probe) != probe {
			addFinding("known_identity", 1)
			knownPrivate = true
		}
		if snapshot.redactor.contactReplacer != nil && snapshot.redactor.contactReplacer.Replace(probe) != probe {
			addFinding("known_contact", 1)
			knownPrivate = true
		}
		if knownPrivate {
			continue
		}
		probe = providerIdentifierToken.ReplaceAllStringFunc(probe, func(id string) string {
			if known[id] {
				return "[VERIFIED_INTERNAL_ID]"
			}
			return id
		})
		safe, excluded := snapshot.Sanitize(probe)
		if excluded != 0 || safe != probe {
			classified := false
			for _, category := range []struct {
				rule    string
				markers []string
			}{
				{"credential", []string{"[敏感片段未纳入分析]", "[凭证片段未纳入分析]"}},
				{"phone", []string{"[电话已脱敏]", "[电话片段已脱敏]", "[联系方式已脱敏]"}},
				{"wechat", []string{"[微信已脱敏]"}},
				{"email", []string{"[邮箱已脱敏]"}},
				{"id_card", []string{"[证件已脱敏]"}},
				{"unknown_name", []string{"[身份片段未纳入分析]", "[家长称谓已脱敏]"}},
				{"oversized", []string{"[超长片段未纳入分析]"}},
			} {
				count := 0
				for _, marker := range category.markers {
					if delta := strings.Count(safe, marker) - strings.Count(probe, marker); delta > 0 {
						count += delta
					}
				}
				if count > 0 {
					addFinding(category.rule, count)
					if category.rule == "unknown_name" {
						findings[len(findings)-1].Hints = nameHints(snapshot.redactor, probe)
					}
					classified = true
				}
			}
			if !classified {
				addFinding("sanitization", 1)
			}
		}
	}
	return findings
}
