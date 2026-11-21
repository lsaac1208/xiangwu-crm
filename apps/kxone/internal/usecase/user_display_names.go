package usecase

import (
	"context"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

func enrichLeadUserNames(ctx context.Context, userBindingRepo repo.UserBindingRepository, leads ...*types.Lead) error {
	if userBindingRepo == nil || len(leads) == 0 {
		return nil
	}

	ids := make([]string, 0, len(leads)*8)
	seen := make(map[string]struct{})
	addID := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	for _, lead := range leads {
		if lead == nil {
			continue
		}
		addID(lead.DispatchManagerUserID)
		addID(lead.AssignedSalesUserID)
		addID(lead.OwnerUserID)
		addID(lead.AssignedInviterUserID)
		addID(lead.InviterUserID)
		addID(lead.CloserUserID)
		addID(lead.CurrentOwnerUserID)
		addID(lead.CreatedByUserID)
		addID(lead.LastDispatchManagerUserID)
	}

	names, err := loadUserDisplayNames(ctx, userBindingRepo, ids)
	if err != nil {
		return err
	}

	for _, lead := range leads {
		if lead == nil {
			continue
		}
		lead.DispatchManagerName = names[lead.DispatchManagerUserID]
		lead.AssignedSalesName = names[lead.AssignedSalesUserID]
		lead.OwnerName = names[lead.OwnerUserID]
		lead.AssignedInviterName = names[lead.AssignedInviterUserID]
		lead.InviterName = names[lead.InviterUserID]
		lead.CloserName = names[lead.CloserUserID]
		lead.CurrentOwnerName = names[lead.CurrentOwnerUserID]
		lead.CreatedByName = names[lead.CreatedByUserID]
		lead.LastDispatchManagerName = names[lead.LastDispatchManagerUserID]
	}

	return nil
}

func loadUserDisplayNames(ctx context.Context, userBindingRepo repo.UserBindingRepository, userIDs []string) (map[string]string, error) {
	names := make(map[string]string, len(userIDs))
	if userBindingRepo == nil {
		return names, nil
	}
	if batchRepo, ok := userBindingRepo.(repo.UserBindingBatchRepository); ok {
		bindings, err := batchRepo.ListByWeComUserIDs(ctx, userIDs)
		if err != nil {
			return nil, err
		}
		for userID, binding := range bindings {
			if binding != nil && binding.EmployeeName != "" {
				names[userID] = binding.EmployeeName
			}
		}
		return names, nil
	}

	seen := make(map[string]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID == "" {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}

		binding, err := userBindingRepo.GetByWeComUserID(ctx, userID)
		if err != nil {
			return nil, err
		}
		if binding != nil && binding.EmployeeName != "" {
			names[userID] = binding.EmployeeName
		}
	}

	return names, nil
}

func displayNameOrID(names map[string]string, userID string) string {
	if names != nil && names[userID] != "" {
		return names[userID]
	}
	return userID
}
