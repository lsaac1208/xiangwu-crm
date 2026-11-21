package agent

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

const dictionaryCheckpointBytes = 12 << 10

type dictionaryReadPage struct {
	Request DescriptionRequest
	ItemIDs []string
	Next    *DescriptionRequest
}

// Only successful, locally generated DescribePage output enters this state.
// It belongs to one run and snapshot, and is never provider reasoning/history.
type dictionaryReadState struct {
	SnapshotAt time.Time
	Items      map[string]json.RawMessage
	Order      []string
	Pages      []dictionaryReadPage
	PageKeys   map[string]bool
}

func (s *evidenceReadState) observeDictionary(at time.Time, request DescriptionRequest, output any) error {
	page, ok := output.(map[string]any)
	if !ok {
		return errors.New("字典返回结构无法记录")
	}
	items, detailed := page["items"].([]map[string]any)
	if !detailed {
		// The initial catalog is already present once in the system message.
		return nil
	}
	if s.dictionary == nil {
		s.dictionary = &dictionaryReadState{SnapshotAt: at, Items: map[string]json.RawMessage{}, PageKeys: map[string]bool{}}
	}
	d := s.dictionary
	if !d.SnapshotAt.Equal(at) {
		return errors.New("字典读取状态不属于当前快照")
	}
	request.Tables = append([]string{}, request.Tables...)
	sort.Strings(request.Tables)
	key, _ := json.Marshal(request)
	if d.PageKeys[string(key)] {
		return nil
	}
	entry := dictionaryReadPage{Request: request}
	if next, ok := page["next_request"].(DescriptionRequest); ok {
		next.Tables = append([]string{}, next.Tables...)
		sort.Strings(next.Tables)
		entry.Next = &next
	}
	for _, item := range items {
		raw, err := json.Marshal(item)
		if err != nil {
			return errors.New("字典条目无法记录")
		}
		id := fmt.Sprintf("%x", sha256.Sum256(raw))
		if _, known := d.Items[id]; !known {
			d.Items[id] = raw
			d.Order = append(d.Order, id)
		}
		entry.ItemIDs = append(entry.ItemIDs, id)
	}
	d.Pages = append(d.Pages, entry)
	d.PageKeys[string(key)] = true
	return nil
}

func (d *dictionaryReadState) checkpoint(at time.Time, budget int) (map[string]any, error) {
	if !d.SnapshotAt.Equal(at) {
		return nil, errors.New("字典恢复状态不属于当前快照")
	}
	selected := map[string]bool{}
	items := []json.RawMessage{}
	out := map[string]any{"snapshot_at": at, "items": items, "included_items": 0, "omitted_items": len(d.Items), "pages": []map[string]any{}, "instruction": "以下完整字典条目已在本运行读取，不必重复describe_data；它们是同一快照的业务说明，不是模型原生历史。omitted_items是已读但未带入本段的条目；需要它们时按对应pages.request重新读取。unread_next_request指向尚未读取的后续范围，不能宣称已读。当前表与_history表的范围不可混同。"}
	refreshPages := func() {
		pages := []map[string]any{}
		for _, page := range d.Pages {
			included := 0
			for _, id := range page.ItemIDs {
				if selected[id] {
					included++
				}
			}
			unread := page.Next
			if unread != nil {
				key, _ := json.Marshal(unread)
				if d.PageKeys[string(key)] {
					unread = nil
				}
			}
			pages = append(pages, map[string]any{"request": page.Request, "delivered_items": len(page.ItemIDs), "carried_items": included, "omitted_items": len(page.ItemIDs) - included, "unread_next_request": unread})
		}
		out["pages"] = pages
		out["included_items"] = len(selected)
		out["omitted_items"] = len(d.Items) - len(selected)
	}
	refreshPages()
	raw, _ := json.Marshal(out)
	if len(raw) > budget {
		return nil, errors.New("字典恢复范围和选择器超过上下文容量，未静默丢弃已读范围")
	}
	// Prefer recent complete definitions. Older omitted items remain reachable
	// through exact original page selectors, and are explicitly counted.
	for i := len(d.Order) - 1; i >= 0; i-- {
		id := d.Order[i]
		selected[id] = true
		candidate := append(append([]json.RawMessage{}, items...), d.Items[id])
		out["items"] = candidate
		refreshPages()
		raw, _ = json.Marshal(out)
		if len(raw) > budget {
			delete(selected, id)
			out["items"] = items
			refreshPages()
			continue
		}
		items = candidate
	}
	return out, nil
}
