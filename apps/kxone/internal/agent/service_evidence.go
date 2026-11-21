package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/gorm"
)

func finalAnswerBlocks(blocks []Block, content string, verification *AnswerVerification, analysisContext AnalysisContext, selected []string, results map[string]*Result) []Block {
	chosen := map[string]bool{}
	for _, id := range selected {
		chosen[id] = true
	}
	out := []Block{}
	for _, b := range blocks {
		if (b.Type == "table" || b.Type == "chart") && chosen[b.ResultID] {
			out = append(out, b)
		}
	}
	out = append(out, Block{Type: "markdown", Text: content})
	sources := []Source{}
	seen := map[string]bool{}
	for _, id := range selected {
		for _, src := range results[id].Sources {
			key := src.Type + ":" + src.ID
			if !seen[key] && len(sources) < 30 {
				seen[key] = true
				sources = append(sources, src)
			}
		}
	}
	return append(out, Block{Type: "sources", Sources: sources, ResultIDs: append([]string{}, selected...), Verification: verification, AnalysisContext: &analysisContext})
}

func (s *Service) loadRunResult(ctx context.Context, runID, resultID string) (*Result, error) {
	var saved ResultRecord
	if err := s.db.WithContext(ctx).Where("id=? AND run_id=?", resultID, runID).First(&saved).Error; err != nil {
		return nil, errors.New("本轮结果读取失败")
	}
	var r Result
	if json.Unmarshal([]byte(saved.Data), &r) != nil {
		return nil, errors.New("结果存储格式错误")
	}
	return &r, nil
}

func (s *Service) readModelResult(ctx context.Context, runID string, p ReadResultRequest) (any, error) {
	r, err := s.loadRunResult(ctx, runID, p.ResultID)
	if err != nil {
		return nil, err
	}
	if p.Row == nil {
		return modelResultPage(r, p.Offset, p.Limit)
	}
	if *p.Row < 0 || *p.Row >= len(r.Rows) {
		return nil, errors.New("文本行超出已保存结果")
	}
	col := -1
	for i, c := range r.Columns {
		if c == p.Column {
			col = i
			break
		}
	}
	if col < 0 || col >= len(r.Rows[*p.Row]) {
		return nil, errors.New("文本列不存在")
	}
	text, ok := r.Rows[*p.Row][col].(string)
	if !ok {
		return nil, errors.New("该单元格不是文本")
	}
	return boundedTextPage(map[string]any{"id": r.ID, "row": *p.Row, "column": p.Column, "snapshot_at": r.SnapshotAt}, text, p.TextOffset)
}

func boundedTextPage(metadata map[string]any, text string, offset int) (map[string]any, error) {
	if offset < 0 || offset > len(text) || offset < len(text) && !isUTF8Start(text[offset]) {
		return nil, errors.New("文本offset不是有效UTF8边界")
	}
	limit := 4096
	for limit > 0 {
		part := boundedUTF8(text[offset:], limit)
		metadata["text"] = part
		metadata["text_offset"] = offset
		metadata["next_text_offset"] = offset + len(part)
		metadata["total_bytes"] = len(text)
		metadata["has_more"] = offset+len(part) < len(text)
		metadata["text_complete"] = offset == 0 && len(part) == len(text)
		raw, _ := json.Marshal(metadata)
		if len(raw) <= modelPageBytes {
			return metadata, nil
		}
		limit /= 2
	}
	return nil, errors.New("文本元数据超过安全页容量")
}
func isUTF8Start(b byte) bool { return b&0xc0 != 0x80 }

func (s *Service) readModelHistory(ctx context.Context, conversationID, messageID string, offset int, snapshot *Snapshot) (any, Message, error) {
	var m Message
	err := s.db.WithContext(ctx).Where("id=? AND conversation_id=? AND (role='user' OR status='completed')", messageID, conversationID).First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, m, errors.New("历史消息不属于当前会话或尚未完成")
		}
		return nil, m, errors.New("历史消息读取失败")
	}
	text, _ := snapshot.Sanitize(m.Content)
	page, err := boundedTextPage(map[string]any{"message_id": m.ID, "role": m.Role, "historical_not_current_evidence": true}, text, offset)
	return page, m, err
}

func runErrorCode(message string) string {
	switch {
	case (strings.Contains(message, "供应商推理上下文含不能发送") || strings.Contains(message, "供应商响应含不能发送")):
		return "provider_privacy"
	case message == ErrSnapshotSourceTimeout.Error():
		return "snapshot_timeout"
	case strings.Contains(message, "供应商拒绝上下文"):
		return "provider_context_limit"
	case strings.Contains(message, "上下文"):
		return "context_capacity"
	case strings.Contains(message, "回答核对") || strings.Contains(message, "最终结果"):
		return "answer_validation_failed"
	case strings.Contains(message, "费用") || strings.Contains(message, "额度"):
		return "budget_limit"
	case strings.Contains(message, "次数上限"):
		return "call_limit"
	case strings.Contains(message, "协议"):
		return "provider_protocol"
	}
	return ""
}
