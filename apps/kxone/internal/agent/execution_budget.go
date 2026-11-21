package agent

import "fmt"

// The execution allowance is rebuilt on the request, not appended to native
// conversation history, so status updates cannot themselves grow the context.
func withExecutionAllowance(messages []ModelMessage, callsLeft, toolsLeft int, correctionUsed bool, pendingReview ...string) []ModelMessage {
	out := append([]ModelMessage{}, messages...)
	if len(out) == 0 {
		return out
	}
	instruction := fmt.Sprintf("\n本次运行剩余模型调用=%d（含本次），剩余工具调用=%d（包含展示、分页和finalize_answer），最终纠正已使用=%t。优先完成用户当前问题，保留最终提交及可能纠正的调用位置；已有数据足够时立即finalize_answer，不为额外结论重复查询。只读当前问题需要的字典页；关键词扩展须确有需要，可合并为一次SQL条件。分组测量已经包含整体事实，不必另做相同集合的无分组测量。", callsLeft, toolsLeft, correctionUsed)
	if callsLeft <= 2 || toolsLeft <= 3 {
		instruction += " 现在收束已有证据；不要新增可选分析或重复读取。用finalize_answer.result_ids选择依据，table_result_ids可同时展示表格；事实没有支持的附加总结直接省略，证据不足明确说明。"
	}
	if toolsLeft <= 1 {
		instruction += " 只余最终提交工具位置；仅可finalize_answer。"
	}
	if correctionUsed && len(pendingReview) > 0 && pendingReview[0] != "" {
		instruction += " 上次最终答案未被接受，待纠正问题（已脱敏）：" + boundedUTF8(pendingReview[0], 2048) + "。本次仍是原运行仅有的纠正机会，不因上下文分段或主备切换重置。"
	}
	if correctionUsed && len(pendingReview) > 1 && pendingReview[1] != "" && len(pendingReview[1]) <= answerRepairBytes {
		// A whole, locally revalidated proposal survives a safe checkpoint. Do
		// not cut JSON or omit the rejected original narrative to make it fit.
		instruction += "\n待确认修复提案JSON：\n" + pendingReview[1]
	}
	out[0].Content += instruction
	return out
}

func toolsForAllowance(toolsLeft int) []any {
	if toolsLeft <= 0 {
		return nil
	}
	all := agentTools()
	if toolsLeft > 1 {
		return all
	}
	for _, item := range all {
		if item.(map[string]any)["function"].(map[string]any)["name"] == "finalize_answer" {
			return []any{item}
		}
	}
	return nil
}
