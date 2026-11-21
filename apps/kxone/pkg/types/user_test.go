package types

import "testing"

func TestInvitationRoleCompatibility(t *testing.T) {
	if !IsInvitationRole(UserRoleInviter) || !IsInvitationRole(UserRoleSales) {
		t.Fatal("inviter and legacy sales must both be invitation roles")
	}
	if IsInvitationRole(UserRoleCloser) {
		t.Fatal("closer must not have invitation permissions")
	}
	for _, role := range []UserRole{UserRoleAdmin, UserRoleManager, UserRoleDispatcher, UserRoleSales, UserRoleInviter, UserRoleCloser} {
		if !IsValidUserRole(role) {
			t.Fatalf("role %s must be valid", role)
		}
	}
	if !CanInitiallyAssignLead(UserRoleDispatcher) || CanInitiallyAssignLead(UserRoleInviter) {
		t.Fatal("dispatcher assignment capability is incorrect")
	}
}

func TestDispatcherLeadScopes(t *testing.T) {
	for _, scope := range []DispatcherLeadScope{DispatcherLeadScopePending, DispatcherLeadScopeHandled, DispatcherLeadScopeAll} {
		if !IsValidDispatcherLeadScope(scope) {
			t.Fatalf("scope %s must be valid", scope)
		}
	}
	if IsValidDispatcherLeadScope("unknown") {
		t.Fatal("unknown dispatcher scope must be rejected")
	}
}
