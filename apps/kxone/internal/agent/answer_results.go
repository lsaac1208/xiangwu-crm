package agent

import (
	"errors"
	"fmt"
)

// AnswerResultSelection separates the final answer's declared evidence from
// exploratory queries. Both sets still refer to saved, immutable run results.
type AnswerResultSelection struct {
	Status           string   `json:"status"`
	ResultIDs        []string `json:"result_ids"`
	ProcessResultIDs []string `json:"process_result_ids"`
	Notice           string   `json:"notice,omitempty"`
}

func ValidateFinalResultIDs(ids []string, results map[string]*Result) error {
	if ids == nil {
		return errors.New("必须明确提供 result_ids 数组；无需采用数据时使用空数组")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || results[id] == nil || results[id].ID != id {
			return fmt.Errorf("最终引用结果 %q 不属于本轮已保存查询", id)
		}
		if seen[id] {
			return fmt.Errorf("最终引用结果 %q 重复", id)
		}
		seen[id] = true
	}
	return nil
}

func ResolveAnswerResults(blocks []Block, results []Result) (AnswerResultSelection, []Result, error) {
	selection := AnswerResultSelection{Status: "unavailable", ResultIDs: []string{}, ProcessResultIDs: []string{}}
	byID := make(map[string]*Result, len(results))
	for i := range results {
		if results[i].ID == "" || byID[results[i].ID] != nil {
			return selection, nil, errors.New("已保存结果的标识缺失或重复")
		}
		byID[results[i].ID] = &results[i]
	}
	explicit := false
	for _, block := range blocks {
		if block.Type != "sources" || block.ResultIDs == nil {
			continue
		}
		if explicit {
			return selection, nil, errors.New("最终回答包含多个结果引用清单")
		}
		explicit = true
		selection.Status = "explicit"
		selection.ResultIDs = append([]string{}, block.ResultIDs...)
	}
	if !explicit {
		seen := map[string]bool{}
		for _, block := range blocks {
			if block.Type != "table" && block.Type != "chart" {
				continue
			}
			if !seen[block.ResultID] {
				seen[block.ResultID] = true
				selection.ResultIDs = append(selection.ResultIDs, block.ResultID)
			}
		}
		if len(selection.ResultIDs) > 0 {
			selection.Status = "legacy_presented"
			selection.Notice = "历史回答未保存完整的最终引用清单；仅采用明确展示的表格或图表，其他查询过程不作为结论导出。"
		} else {
			selection.Notice = "本次回答未保存最终引用结果；仅导出回答文字，查询过程不作为结论导出。"
		}
	}
	if err := ValidateFinalResultIDs(selection.ResultIDs, byID); err != nil {
		return selection, nil, err
	}
	selected := make([]Result, 0, len(selection.ResultIDs))
	chosen := map[string]bool{}
	for _, id := range selection.ResultIDs {
		chosen[id] = true
		selected = append(selected, *byID[id])
	}
	for _, block := range blocks {
		if (block.Type == "table" || block.Type == "chart") && !chosen[block.ResultID] {
			return selection, nil, fmt.Errorf("展示结果 %q 未列入最终引用清单", block.ResultID)
		}
	}
	for _, result := range results {
		if !chosen[result.ID] {
			selection.ProcessResultIDs = append(selection.ProcessResultIDs, result.ID)
		}
	}
	if explicit && len(selection.ResultIDs) == 0 {
		selection.Notice = "本次明确未采用查询结果；查询过程不作为结论导出。"
	}
	return selection, selected, nil
}

func ResolveRunAnswerResults(status string, blocks []Block, results []Result) (AnswerResultSelection, []Result, error) {
	if status == "completed" {
		return ResolveAnswerResults(blocks, results)
	}
	selection, selected, err := ResolveAnswerResults(nil, results)
	selection.Status = "unfinished"
	selection.Notice = "本轮分析未完成，已有文字仅供核对；查询结果尚未最终确认，不作为结论导出。"
	return selection, selected, err
}
