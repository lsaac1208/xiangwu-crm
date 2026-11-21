package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/google/uuid"
)

// A capability exists only in this execution's memory, after the entire neutral
// proposal passed the regular evidence/answer/condition gates. It is neither a
// published answer nor permission to replace its underlying references.
type answerRepairGrant struct {
	id          string
	runID       string
	requestJSON []byte
	binding     [32]byte
	used        bool
}

type acceptAnswerRepairRequest struct {
	ProposalID  string `json:"proposal_id"`
	Explanation string `json:"explanation,omitempty"`
}

func answerRepairBinding(runID string, req FinalizeRequest, selected []string, results map[string]*Result, reads *evidenceReadState, conditions AnalysisContext, snapshot *Snapshot) ([32]byte, error) {
	var empty [32]byte
	if runID == "" || snapshot == nil || reads == nil {
		return empty, errors.New("修订提案缺少本轮证据")
	}
	snapshot.mu.Lock()
	closed := snapshot.closed
	snapshot.mu.Unlock()
	if closed {
		return empty, errors.New("修订提案快照已关闭")
	}
	if err := ValidateFinalResultIDs(req.ResultIDs, results); err != nil {
		return empty, err
	}
	boundResults := map[string]*Result{}
	for _, id := range req.ResultIDs {
		result := results[id]
		if result.ID != id || !result.SnapshotAt.Equal(snapshot.At) {
			return empty, errors.New("修订提案结果不属于当前快照")
		}
		boundResults[id] = result
	}
	h := sha256.New()
	// Include the delivered cell values, not just their IDs: the answer renderer
	// uses those pages and may attach their row labels to a referenced value.
	err := json.NewEncoder(h).Encode(map[string]any{"run": runID, "snapshot": snapshot.At, "request": req, "selection": selected, "results": boundResults, "conditions": conditions, "read_results": reads.results, "read_cells": reads.cells, "read_values": reads.values, "read_facts": reads.facts})
	if err != nil {
		return empty, errors.New("修订提案证据无法固定")
	}
	copy(empty[:], h.Sum(nil))
	return empty, nil
}

func newAnswerRepairGrant(proposal *answerRepairProposal, selected []string, results map[string]*Result, reads *evidenceReadState, conditions AnalysisContext, history []Message, snapshot *Snapshot, runID string) (*answerRepairGrant, error) {
	if proposal == nil || reads == nil || snapshot == nil {
		return nil, errors.New("没有已核对的修订提案")
	}
	// Recheck, rather than trusting the caller or an exported proposal object.
	if err := reads.validate(proposal.Request.References); err != nil {
		return nil, err
	}
	if _, _, err := ValidateAnswer(proposal.Request, selected, results, reads); err != nil {
		return nil, err
	}
	if _, err := mergeConditions(conditions, proposal.Request.Conditions, history, snapshot, runID); err != nil {
		return nil, err
	}
	binding, err := answerRepairBinding(runID, proposal.Request, selected, results, reads, conditions, snapshot)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(proposal.Request)
	if err != nil {
		return nil, err
	}
	grant := &answerRepairGrant{id: uuid.NewString(), runID: runID, requestJSON: raw, binding: binding}
	proposal.ProposalID = grant.id
	proposal.Instruction = "此提案仅固定已核对引用，不证明原解释成立或用户问题已经完整回答。withheld_markdown是未采纳的草稿材料，不能照抄其中的数字、范围或时间推论。确认保留依据支持所答内容后，显式调用accept_answer_repair，proposal_id使用本提案标识；explanation可写必要解释或证据不足，不手写数字/数量/表格或时间先后。服务端保留结果、引用、条件并再次核验。不能重交finalize_answer，也不能仅回复确认；原调用、工具、费用、时限和唯一纠正机会不变。"
	return grant, nil
}

func (g *answerRepairGrant) accept(ctx context.Context, arguments, runID string, selected []string, results map[string]*Result, reads *evidenceReadState, conditions AnalysisContext, history []Message, snapshot *Snapshot) (FinalizeRequest, error) {
	var empty FinalizeRequest
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if g == nil || g.used || g.runID != runID {
		return empty, errors.New("修订提案不属于本轮或已使用")
	}
	g.used = true // even a rejected acceptance cannot create another correction.
	if len(arguments) > answerRepairBytes {
		return empty, errors.New("修订提交超过容量")
	}
	var input acceptAnswerRepairRequest
	decoder := json.NewDecoder(strings.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.ProposalID != g.id {
		return empty, errors.New("修订提案标识或参数无效；只能提交proposal_id和explanation")
	}
	var req FinalizeRequest
	if json.Unmarshal(g.requestJSON, &req) != nil {
		return empty, errors.New("修订提案无法读取")
	}
	binding, err := answerRepairBinding(runID, req, selected, results, reads, conditions, snapshot)
	if err != nil {
		return empty, err
	}
	if !bytes.Equal(binding[:], g.binding[:]) {
		return empty, errors.New("修订提案依据已变化，不能沿用旧核验")
	}
	if err = reads.validate(req.References); err != nil {
		return empty, err
	}
	if _, err = mergeConditions(conditions, req.Conditions, history, snapshot, runID); err != nil {
		return empty, err
	}
	if len(input.Explanation) > 4096 {
		return empty, errors.New("修订解释超过容量")
	}
	if len(providerPrivacyFindings(snapshot, ModelMessage{Content: input.Explanation}, results)) > 0 {
		return empty, errors.New("修订解释含不能发送的敏感片段")
	}
	explanation, excluded := snapshot.Sanitize(input.Explanation)
	if excluded > 0 {
		return empty, errors.New("修订解释含不能发送的敏感片段")
	}
	if strings.TrimSpace(explanation) != "" {
		req.Markdown += "\n\n" + explanation
	}
	// Preserve the ordinary narrative, numeric, temporal and scope gates. The
	// service repeats validation before publication and checks authorization.
	if _, _, err = ValidateAnswer(req, selected, results, reads); err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	return req, nil
}

func toolsForAnswerRepair(grant *answerRepairGrant, toolsLeft int) []any {
	if grant == nil {
		return toolsForAllowance(toolsLeft)
	}
	if toolsLeft <= 0 {
		return nil
	}
	return []any{tool("accept_answer_repair", "唯一纠正提交：确认采用已核对的修订提案并再次校验；不能重新提交原草稿、结果或引用。先核对withheld_markdown和用户需求；必要解释或证据不足放explanation，不手写数字、数量、数据表、全称范围或时间先后。示例：{\"proposal_id\":\"本工具enum中的真实标识\",\"explanation\":\"已按请求范围核对，业务含义以引用指标为准。\"}。本工具必须是此次唯一工具；成功后立即完成。", map[string]any{"proposal_id": map[string]any{"type": "string", "enum": []string{grant.id}}, "explanation": map[string]any{"type": "string", "maxLength": 4096, "description": "可选，经过证据核对的解释或明确说明缺少依据；不得照抄withheld_markdown中的未绑定数值；不提供时仅展示原中性说明和已核对引用。"}}, "proposal_id")}
}

func withAnswerRepairInstructions(messages []ModelMessage, grant *answerRepairGrant) []ModelMessage {
	if grant == nil || len(messages) == 0 {
		return messages
	}
	out := append([]ModelMessage{}, messages...)
	out[0].Content = "本轮现处于唯一修订确认阶段，工具已收束为accept_answer_repair；此前通用finalize_answer指引不适用于此阶段。逐项核对待确认提案与用户需求，显式选择proposal_id；需要说明的业务含义或证据缺口写在explanation，不重抄原草稿数字/数量/时间推论。不接受仅回复确认，不执行额外查询。\n" + out[0].Content
	return out
}
