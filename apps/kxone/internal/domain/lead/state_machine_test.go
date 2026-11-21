package lead

import (
	"testing"

	"github.com/lsaac128/kxone/pkg/types"
)

func TestStateMachine_CanTransition(t *testing.T) {
	sm := NewStateMachine()

	tests := []struct {
		name          string
		currentStatus types.LeadStatus
		event         types.LeadEvent
		expected      bool
	}{
		{
			name:          "pending_dispatch to assigned",
			currentStatus: types.LeadStatusPendingDispatch,
			event:         types.LeadEventAssignedToSales,
			expected:      true,
		},
		{
			name:          "assigned to claimed",
			currentStatus: types.LeadStatusAssignedPendingClaim,
			event:         types.LeadEventClaimedBySales,
			expected:      true,
		},
		{
			name:          "claimed to following",
			currentStatus: types.LeadStatusClaimed,
			event:         types.LeadEventFollowupSubmitted,
			expected:      true,
		},
		{
			name:          "claimed to converted",
			currentStatus: types.LeadStatusClaimed,
			event:         types.LeadEventConverted,
			expected:      true,
		},
		{
			name:          "invalid transition: pending_dispatch to claimed",
			currentStatus: types.LeadStatusPendingDispatch,
			event:         types.LeadEventClaimedBySales,
			expected:      false,
		},
		{
			name:          "invalid transition: claimed to assigned",
			currentStatus: types.LeadStatusClaimed,
			event:         types.LeadEventAssignedToSales,
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sm.CanTransition(tt.currentStatus, tt.event)
			if result != tt.expected {
				t.Errorf("CanTransition(%v, %v) = %v, want %v",
					tt.currentStatus, tt.event, result, tt.expected)
			}
		})
	}
}

func TestStateMachine_NextState(t *testing.T) {
	sm := NewStateMachine()

	tests := []struct {
		name           string
		currentStatus  types.LeadStatus
		event          types.LeadEvent
		expectedStatus types.LeadStatus
		expectError    bool
	}{
		{
			name:           "assigned to sales",
			currentStatus:  types.LeadStatusPendingDispatch,
			event:          types.LeadEventAssignedToSales,
			expectedStatus: types.LeadStatusAssignedPendingClaim,
			expectError:    false,
		},
		{
			name:           "claimed by sales",
			currentStatus:  types.LeadStatusAssignedPendingClaim,
			event:          types.LeadEventClaimedBySales,
			expectedStatus: types.LeadStatusClaimed,
			expectError:    false,
		},
		{
			name:           "followup submitted",
			currentStatus:  types.LeadStatusClaimed,
			event:          types.LeadEventFollowupSubmitted,
			expectedStatus: types.LeadStatusFollowing,
			expectError:    false,
		},
		{
			name:           "claimed to converted",
			currentStatus:  types.LeadStatusClaimed,
			event:          types.LeadEventConverted,
			expectedStatus: types.LeadStatusConverted,
			expectError:    false,
		},
		{
			name:           "invalid transition: pending_dispatch to claimed",
			currentStatus:  types.LeadStatusPendingDispatch,
			event:          types.LeadEventClaimedBySales,
			expectedStatus: types.LeadStatusPendingDispatch,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextStatus, err := sm.NextState(tt.currentStatus, tt.event)

			if tt.expectError {
				if err == nil {
					t.Errorf("NextState(%v, %v) expected error but got nil",
						tt.currentStatus, tt.event)
				}
			} else {
				if err != nil {
					t.Errorf("NextState(%v, %v) unexpected error: %v",
						tt.currentStatus, tt.event, err)
				}
				if nextStatus != tt.expectedStatus {
					t.Errorf("NextState(%v, %v) = %v, want %v",
						tt.currentStatus, tt.event, nextStatus, tt.expectedStatus)
				}
			}
		})
	}
}

func TestStateMachine_TerminalStatusesDoNotTransition(t *testing.T) {
	sm := NewStateMachine()

	for _, status := range []types.LeadStatus{
		types.LeadStatusInvalid,
		types.LeadStatusConverted,
		types.LeadStatusClosed,
	} {
		t.Run(string(status), func(t *testing.T) {
			if sm.CanTransition(status, types.LeadEventReassigned) {
				t.Fatalf("terminal status %s should not transition", status)
			}
			next, err := sm.NextState(status, types.LeadEventReassigned)
			if err != types.ErrLeadStatusInvalid {
				t.Fatalf("err = %v, want %v", err, types.ErrLeadStatusInvalid)
			}
			if next != status {
				t.Fatalf("next = %s, want %s", next, status)
			}
		})
	}
}

func TestStateMachine_ValidateClaim(t *testing.T) {
	sm := NewStateMachine()

	tests := []struct {
		name        string
		lead        *types.Lead
		salesUserID string
		expectError error
	}{
		{
			name: "valid claim",
			lead: &types.Lead{
				Status:              types.LeadStatusAssignedPendingClaim,
				AssignedSalesUserID: "sales001",
			},
			salesUserID: "sales001",
			expectError: nil,
		},
		{
			name: "already claimed",
			lead: &types.Lead{
				Status:              types.LeadStatusClaimed,
				AssignedSalesUserID: "sales001",
			},
			salesUserID: "sales001",
			expectError: types.ErrLeadAlreadyClaimed,
		},
		{
			name: "wrong sales user",
			lead: &types.Lead{
				Status:              types.LeadStatusAssignedPendingClaim,
				AssignedSalesUserID: "sales001",
			},
			salesUserID: "sales002",
			expectError: types.ErrSalesNotAssigned,
		},
		{
			name: "wrong status",
			lead: &types.Lead{
				Status:              types.LeadStatusFollowing,
				AssignedSalesUserID: "sales001",
			},
			salesUserID: "sales001",
			expectError: types.ErrLeadStatusInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sm.ValidateClaim(tt.lead, tt.salesUserID)

			if tt.expectError != nil {
				if err != tt.expectError {
					t.Errorf("ValidateClaim() error = %v, want %v", err, tt.expectError)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateClaim() unexpected error: %v", err)
				}
			}
		})
	}
}
