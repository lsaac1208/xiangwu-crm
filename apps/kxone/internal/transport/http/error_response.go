package http

import (
	"errors"
	"log"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/usecase"
	"github.com/lsaac128/kxone/pkg/types"
)

func respondInvalidRequest(c *gin.Context, err error) {
	if IsRequestBodyTooLarge(err) {
		respondError(c, stdhttp.StatusRequestEntityTooLarge, "request_body_too_large", err)
		return
	}
	respondError(c, stdhttp.StatusBadRequest, "invalid_request", err)
}

func invalidRequest(c *gin.Context) {
	respondError(c, stdhttp.StatusBadRequest, "invalid_request", nil)
}

func respondInternalError(c *gin.Context, code string, err error) {
	respondError(c, stdhttp.StatusInternalServerError, code, err)
}

func internalError(c *gin.Context, status int, code string, err error) {
	respondError(c, status, code, err)
}

func respondUseCaseError(c *gin.Context, err error, fallbackCode string) {
	status, code, known := classifyUseCaseError(err)
	if known {
		respondError(c, status, code, err)
		return
	}
	respondInternalError(c, fallbackCode, err)
}

func respondError(c *gin.Context, status int, code string, err error) {
	if err != nil {
		logHTTPError(c, code, err)
	}
	c.JSON(status, gin.H{"error": code})
}

func logHTTPError(c *gin.Context, code string, err error) {
	if err == nil {
		return
	}
	log.Printf("[HTTP] %s %s failed: code=%s err=%v", c.Request.Method, requestPath(c), code, err)
}

func requestPath(c *gin.Context) string {
	if path := c.FullPath(); path != "" {
		return path
	}
	if c.Request != nil && c.Request.URL != nil {
		return c.Request.URL.Path
	}
	return ""
}

func classifyUseCaseError(err error) (int, string, bool) {
	switch {
	case errors.Is(err, types.ErrLeadNotFound):
		return stdhttp.StatusNotFound, "lead_not_found", true
	case errors.Is(err, types.ErrLeadStatusInvalid):
		return stdhttp.StatusBadRequest, "invalid_lead_status", true
	case errors.Is(err, types.ErrInvalidReasonRequired):
		return stdhttp.StatusBadRequest, "invalid_reason_required", true
	case errors.Is(err, types.ErrLeadAlreadyClaimed):
		return stdhttp.StatusConflict, "lead_already_claimed", true
	case errors.Is(err, types.ErrLeadReassigned):
		return stdhttp.StatusConflict, "lead_reassigned", true
	case errors.Is(err, types.ErrInvalidIdempotencyKey):
		return stdhttp.StatusUnprocessableEntity, "invalid_idempotency_key", true
	case errors.Is(err, types.ErrIdempotencyConflict):
		return stdhttp.StatusConflict, "idempotency_conflict", true
	case errors.Is(err, types.ErrLeadVersionConflict):
		return stdhttp.StatusConflict, "lead_version_conflict", true
	case errors.Is(err, types.ErrUnauthorized):
		return stdhttp.StatusForbidden, "forbidden", true
	case errors.Is(err, types.ErrSalesNotAssigned):
		return stdhttp.StatusBadRequest, "sales_not_assigned", true
	case errors.Is(err, types.ErrDuplicateClaim):
		return stdhttp.StatusConflict, "duplicate_claim", true
	case errors.Is(err, types.ErrUserBindingNotFound):
		return stdhttp.StatusNotFound, "user_binding_not_found", true
	case errors.Is(err, types.ErrInvalidInviter):
		return stdhttp.StatusBadRequest, "invalid_inviter", true
	case errors.Is(err, types.ErrInvalidStarRating):
		return stdhttp.StatusBadRequest, "invalid_star_rating", true
	case errors.Is(err, types.ErrInvalidFollowupDue):
		return stdhttp.StatusBadRequest, "invalid_followup_due", true
	case errors.Is(err, types.ErrLeadNotInPublicPool):
		return stdhttp.StatusConflict, "lead_not_in_public_pool", true
	case errors.Is(err, types.ErrPublicPoolForbidden):
		return stdhttp.StatusBadRequest, "public_pool_forbidden", true
	case errors.Is(err, types.ErrPublicPoolCapacity):
		return stdhttp.StatusConflict, "public_pool_capacity_reached", true
	case errors.Is(err, types.ErrPublicPoolCooldown):
		return stdhttp.StatusConflict, "public_pool_cooldown", true
	case errors.Is(err, types.ErrAppointmentForbidden):
		return stdhttp.StatusBadRequest, "appointment_forbidden", true
	case errors.Is(err, types.ErrAppointmentNotFound):
		return stdhttp.StatusNotFound, "appointment_not_found", true
	case errors.Is(err, types.ErrAppointmentAlreadyActive):
		return stdhttp.StatusConflict, "appointment_already_active", true
	case errors.Is(err, types.ErrAppointmentTimeInvalid):
		return stdhttp.StatusBadRequest, "appointment_time_invalid", true
	case errors.Is(err, types.ErrAppointmentDetailsRequired):
		return stdhttp.StatusBadRequest, "appointment_details_required", true
	case errors.Is(err, types.ErrAppointmentStatusInvalid):
		return stdhttp.StatusConflict, "appointment_status_invalid", true
	case errors.Is(err, types.ErrAppointmentVersionConflict):
		return stdhttp.StatusConflict, "appointment_version_conflict", true
	case errors.Is(err, types.ErrInvalidCloser):
		return stdhttp.StatusBadRequest, "invalid_closer", true
	case errors.Is(err, types.ErrConflictOverrideRequired):
		return stdhttp.StatusBadRequest, "conflict_override_reason_required", true
	case errors.Is(err, types.ErrAvailabilityInvalid):
		return stdhttp.StatusBadRequest, "availability_invalid", true
	case errors.Is(err, types.ErrDeliveryForbidden):
		return stdhttp.StatusConflict, "delivery_forbidden", true
	case errors.Is(err, types.ErrDeliveryAlreadyCompleted):
		return stdhttp.StatusConflict, "delivery_already_completed", true
	case errors.Is(err, types.ErrNegotiationReassignInvalid):
		return stdhttp.StatusBadRequest, "negotiation_reassign_invalid", true
	case errors.Is(err, types.ErrInvalidDispatcherScope):
		return stdhttp.StatusBadRequest, "invalid_dispatcher_scope", true
	case errors.Is(err, types.ErrInvalidLeadProfile):
		return stdhttp.StatusUnprocessableEntity, "invalid_lead_profile", true
	case errors.Is(err, types.ErrInvalidLeadSort):
		return stdhttp.StatusBadRequest, "invalid_lead_sort", true
	case errors.Is(err, types.ErrInvalidLeadPreference):
		return stdhttp.StatusBadRequest, "invalid_lead_preference", true
	case errors.Is(err, types.ErrLeadPreferenceConflict):
		return stdhttp.StatusConflict, "lead_preference_conflict", true
	case errors.Is(err, types.ErrLeadViewNotFound):
		return stdhttp.StatusNotFound, "lead_view_not_found", true
	case errors.Is(err, types.ErrLeadViewLimit):
		return stdhttp.StatusConflict, "lead_view_limit", true
	case errors.Is(err, types.ErrLeadViewNameConflict):
		return stdhttp.StatusConflict, "lead_view_name_conflict", true
	case errors.Is(err, types.ErrInvalidBatchLeadRequest):
		return stdhttp.StatusBadRequest, "invalid_batch_lead_request", true
	case errors.Is(err, usecase.ErrNoSalesAvailable):
		return stdhttp.StatusBadRequest, "no_sales_available", true
	default:
		return stdhttp.StatusInternalServerError, "", false
	}
}
