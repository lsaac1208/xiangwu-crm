package usecase

import (
	"path/filepath"
	"testing"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestEnrichLeadUserNamesIncludesDualStageOwners(t *testing.T) {
	db, err := repo.NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "names.db"), MaxConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	bindings := repo.NewGormUserBindingRepository(db)
	for index, binding := range []*types.WeComUserBinding{
		{CRMUserID: 101, WeComUserID: "inviter01", EmployeeName: "真实邀约师", Role: types.UserRoleInviter, Status: types.UserStatusActive},
		{CRMUserID: 102, WeComUserID: "closer01", EmployeeName: "真实谈单师", Role: types.UserRoleCloser, Status: types.UserStatusActive},
	} {
		binding.ID = int64(index + 1)
		if err := bindings.Create(t.Context(), binding); err != nil {
			t.Fatal(err)
		}
	}
	lead := &types.Lead{
		AssignedInviterUserID: "inviter01",
		InviterUserID:         "inviter01",
		CloserUserID:          "closer01",
		CurrentOwnerUserID:    "closer01",
	}
	if err := enrichLeadUserNames(t.Context(), bindings, lead); err != nil {
		t.Fatal(err)
	}
	if lead.AssignedInviterName != "真实邀约师" || lead.InviterName != "真实邀约师" {
		t.Fatalf("inviter names = %q, %q", lead.AssignedInviterName, lead.InviterName)
	}
	if lead.CloserName != "真实谈单师" || lead.CurrentOwnerName != "真实谈单师" {
		t.Fatalf("closer names = %q, %q", lead.CloserName, lead.CurrentOwnerName)
	}
}
