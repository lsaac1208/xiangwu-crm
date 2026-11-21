package notification

import (
	"context"
	"testing"

	"github.com/lsaac128/kxone/pkg/types"
)

type mentionBindingReaderStub struct {
	items map[string]*types.WeComUserBinding
}

func (s *mentionBindingReaderStub) GetByWeComUserID(_ context.Context, id string) (*types.WeComUserBinding, error) {
	return s.items[id], nil
}
func (s *mentionBindingReaderStub) List(_ context.Context, status, role string, offset, limit int) ([]*types.WeComUserBinding, int, error) {
	var result []*types.WeComUserBinding
	for _, item := range s.items {
		if item.Status == types.UserStatusActive && string(item.Role) == role && status == string(types.UserStatusActive) {
			result = append(result, item)
		}
	}
	return result, len(result), nil
}

func TestResolveMentionRecipientsUsesSeparateActiveUserIDAndDeduplicates(t *testing.T) {
	reader := &mentionBindingReaderStub{items: map[string]*types.WeComUserBinding{
		"actor-1": {WeComUserID: "actor-1", EmployeeName: "张三", Status: types.UserStatusActive, GroupRobotMentionUserID: "wx-1"},
		"actor-2": {WeComUserID: "actor-2", EmployeeName: "李四", Status: types.UserStatusDisabled, GroupRobotMentionUserID: "wx-2"},
	}}
	got, err := ResolveMentionRecipients(context.Background(), reader, []string{"actor-1", "actor-1", "actor-2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.MentionUserIDs) != 1 || got.MentionUserIDs[0] != "wx-1" {
		t.Fatalf("mentions = %#v", got.MentionUserIDs)
	}
	if len(got.MissingNames) != 1 || got.MissingNames[0] != "李四" {
		t.Fatalf("missing = %#v", got.MissingNames)
	}
}

func TestResolveMentionRecipientsKeepsMessageWhenBindingMissing(t *testing.T) {
	got, err := ResolveMentionRecipients(context.Background(), nil, []string{"actor-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.MentionUserIDs) != 0 || len(got.MissingNames) != 1 {
		t.Fatalf("unexpected result: %#v", got)
	}
}
