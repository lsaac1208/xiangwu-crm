package notification

import (
	"context"
	"strings"

	"github.com/lsaac128/kxone/pkg/types"
)

const recipientPageSize = 200

type BindingLister interface {
	List(ctx context.Context, status, role string, offset, limit int) ([]*types.WeComUserBinding, int, error)
}

type MentionBindingReader interface {
	BindingLister
	GetByWeComUserID(ctx context.Context, wecomUserID string) (*types.WeComUserBinding, error)
}

type MentionRecipients struct {
	MentionUserIDs []string
	DisplayNames   []string
	MissingNames   []string
}

// ResolveMentionRecipients maps immutable CRM actor IDs to the separately
// configured enterprise-WeChat UserIDs used only for group robot mentions.
func ResolveMentionRecipients(ctx context.Context, bindings MentionBindingReader, actorIDs []string) (*MentionRecipients, error) {
	result := &MentionRecipients{}
	if bindings == nil {
		for _, actorID := range actorIDs {
			if name := strings.TrimSpace(actorID); name != "" {
				result.DisplayNames = append(result.DisplayNames, name)
				result.MissingNames = append(result.MissingNames, name)
			}
		}
		return result, nil
	}
	seenMention := map[string]struct{}{}
	seenNames := map[string]struct{}{}
	for _, actorID := range actorIDs {
		actorID = strings.TrimSpace(actorID)
		if actorID == "" {
			continue
		}
		binding, err := bindings.GetByWeComUserID(ctx, actorID)
		if err != nil {
			return nil, err
		}
		name := actorID
		if binding != nil && strings.TrimSpace(binding.EmployeeName) != "" {
			name = strings.TrimSpace(binding.EmployeeName)
		}
		if _, ok := seenNames[name]; !ok {
			seenNames[name] = struct{}{}
			result.DisplayNames = append(result.DisplayNames, name)
		}
		if binding == nil || binding.Status != types.UserStatusActive || strings.TrimSpace(binding.GroupRobotMentionUserID) == "" {
			result.MissingNames = append(result.MissingNames, name)
			continue
		}
		mentionID := strings.TrimSpace(binding.GroupRobotMentionUserID)
		if _, ok := seenMention[mentionID]; ok {
			continue
		}
		seenMention[mentionID] = struct{}{}
		result.MentionUserIDs = append(result.MentionUserIDs, mentionID)
	}
	return result, nil
}

func ActiveManagerUserIDs(ctx context.Context, bindings BindingLister) ([]string, error) {
	return activeRoleUserIDs(ctx, bindings, types.UserRoleManager)
}

func activeRoleUserIDs(ctx context.Context, bindings BindingLister, role types.UserRole) ([]string, error) {
	if bindings == nil {
		return nil, nil
	}
	items, _, err := bindings.List(ctx, string(types.UserStatusActive), string(role), 0, recipientPageSize)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item != nil && strings.TrimSpace(item.WeComUserID) != "" {
			result = append(result, strings.TrimSpace(item.WeComUserID))
		}
	}
	return result, nil
}

// ActiveAdminManagerUserIDs returns every active CRM administrator and sales manager.
func ActiveAdminManagerUserIDs(ctx context.Context, bindings BindingLister) ([]string, error) {
	if bindings == nil {
		return nil, nil
	}
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, role := range []types.UserRole{types.UserRoleAdmin, types.UserRoleManager} {
		for offset := 0; ; offset += recipientPageSize {
			items, total, err := bindings.List(ctx, string(types.UserStatusActive), string(role), offset, recipientPageSize)
			if err != nil {
				return nil, err
			}
			for _, item := range items {
				if item == nil {
					continue
				}
				userID := strings.TrimSpace(item.WeComUserID)
				if userID == "" {
					continue
				}
				if _, exists := seen[userID]; exists {
					continue
				}
				seen[userID] = struct{}{}
				result = append(result, userID)
			}
			if len(items) == 0 || offset+len(items) >= total {
				break
			}
		}
	}
	return result, nil
}
