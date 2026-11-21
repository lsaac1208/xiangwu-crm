package usecase

import (
	"testing"

	"github.com/lsaac128/kxone/pkg/types"
)

func TestApplyLeadMobileVisibility(t *testing.T) {
	baseLead := func() *types.Lead {
		return &types.Lead{
			CustomerMobile:        "13912345678",
			CustomerMobileMasked:  "13912345678",
			CustomerWechat:        "wx-secret-value",
			CustomerWechatMasked:  "wx-secret-value",
			DispatchManagerUserID: "manager_001",
			AssignedSalesUserID:   "sales_001",
			OwnerUserID:           "sales_001",
		}
	}

	tests := []struct {
		name       string
		userID     string
		role       string
		wantMobile string
	}{
		{name: "admin sees full mobile", userID: "admin_001", role: "admin", wantMobile: "13912345678"},
		{name: "dispatch manager sees full mobile", userID: "manager_001", role: "manager", wantMobile: "13912345678"},
		{name: "owner sales sees full mobile", userID: "sales_001", role: "sales", wantMobile: "13912345678"},
		{name: "other sales is masked by backend", userID: "sales_002", role: "sales", wantMobile: ""},
		{name: "other manager sees full mobile", userID: "manager_002", role: "manager", wantMobile: "13912345678"},
		{name: "dispatcher is always safely masked", userID: "dispatcher_001", role: "dispatcher", wantMobile: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lead := baseLead()
			applyLeadMobileVisibility(lead, tt.userID, tt.role)
			if lead.CustomerMobile != tt.wantMobile {
				t.Fatalf("customer mobile = %q, want %q", lead.CustomerMobile, tt.wantMobile)
			}
			if tt.role == string(types.UserRoleDispatcher) {
				if lead.CustomerMobileMasked != "139****5678" || lead.CustomerWechatMasked != "wx****ue" || lead.CustomerWechat != "" {
					t.Fatalf("dispatcher masks = %q/%q raw wechat=%q", lead.CustomerMobileMasked, lead.CustomerWechatMasked, lead.CustomerWechat)
				}
			} else if tt.wantMobile == "" && lead.CustomerMobileMasked != "139****5678" {
				t.Fatalf("restricted mobile mask = %q", lead.CustomerMobileMasked)
			} else if tt.wantMobile != "" && lead.CustomerMobileMasked != "13912345678" {
				t.Fatalf("masked mobile unexpectedly changed: %q", lead.CustomerMobileMasked)
			}
		})
	}
}
