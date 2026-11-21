package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/usecase"
	"github.com/lsaac128/kxone/pkg/types"
)

type AppointmentHandler struct {
	appointments *usecase.AppointmentUseCase
	availability *usecase.AvailabilityUseCase
}

func NewAppointmentHandler(appointments *usecase.AppointmentUseCase, availability *usecase.AvailabilityUseCase) *AppointmentHandler {
	return &AppointmentHandler{appointments: appointments, availability: availability}
}

func (h *AppointmentHandler) Create(c *gin.Context) {
	var body struct {
		LeadID           int64  `json:"lead_id" binding:"required"`
		StartAt          string `json:"start_at" binding:"required"`
		EndAt            string `json:"end_at" binding:"required"`
		BusinessTimezone string `json:"business_timezone"`
		Campus           string `json:"campus" binding:"required"`
		VisitorCount     int    `json:"visitor_count"`
		CustomerConcerns string `json:"customer_concerns"`
		HandoffNotes     string `json:"handoff_notes" binding:"required"`
		SpecialNotes     string `json:"special_notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	startAt, endAt, ok := parseTimeRange(c, body.StartAt, body.EndAt)
	if !ok {
		return
	}
	actor, role, ok := actorIdentity(c)
	if !ok {
		return
	}
	appointment, err := h.appointments.Create(c.Request.Context(), &usecase.CreateAppointmentRequest{LeadID: body.LeadID, ActorUserID: actor, ActorRole: role, StartAt: startAt, EndAt: endAt, BusinessTimezone: body.BusinessTimezone, Campus: body.Campus, VisitorCount: body.VisitorCount, CustomerConcerns: body.CustomerConcerns, HandoffNotes: body.HandoffNotes, SpecialNotes: body.SpecialNotes})
	if err != nil {
		respondUseCaseError(c, err, "create_appointment_failed")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"appointment": appointment})
}

func (h *AppointmentHandler) List(c *gin.Context) {
	actor, role, ok := actorIdentity(c)
	if !ok {
		return
	}
	startAt, endAt, ok := parseOptionalRange(c)
	if !ok {
		return
	}
	leadID, _ := strconv.ParseInt(c.Query("lead_id"), 10, 64)
	items, err := h.appointments.List(c.Request.Context(), &usecase.ListAppointmentsRequest{ActorUserID: actor, ActorRole: role, LeadID: leadID, CloserUserID: strings.TrimSpace(c.Query("closer_userid")), Status: strings.TrimSpace(c.Query("status")), StartAt: startAt, EndAt: endAt})
	if err != nil {
		respondUseCaseError(c, err, "list_appointments_failed")
		return
	}
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.LeadID)
	}
	RecordLeadReadIDs(c, ids...)
	c.JSON(http.StatusOK, gin.H{"appointments": items, "total": len(items)})
}

func (h *AppointmentHandler) Get(c *gin.Context) {
	id, ok := parseInt64Param(c, "id", "invalid_appointment_id")
	if !ok {
		return
	}
	actor, role, ok := actorIdentity(c)
	if !ok {
		return
	}
	appointment, events, err := h.appointments.Get(c.Request.Context(), id, actor, role)
	if err != nil {
		respondUseCaseError(c, err, "get_appointment_failed")
		return
	}
	RecordLeadReadIDs(c, appointment.LeadID)
	c.JSON(http.StatusOK, gin.H{"appointment": appointment, "events": events})
}

func (h *AppointmentHandler) AssignCloser(c *gin.Context) {
	id, ok := parseInt64Param(c, "id", "invalid_appointment_id")
	if !ok {
		return
	}
	var body struct {
		CloserUserID      string `json:"closer_userid" binding:"required"`
		ExpectedVersion   int    `json:"expected_version"`
		ForceSoftConflict bool   `json:"force_soft_conflicts"`
		OverrideReason    string `json:"override_reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	actor, role, ok := actorIdentity(c)
	if !ok {
		return
	}
	conflicts, err := h.appointments.AssignCloser(c.Request.Context(), &usecase.AssignCloserRequest{AppointmentID: id, ActorUserID: actor, ActorRole: role, CloserUserID: body.CloserUserID, ExpectedVersion: body.ExpectedVersion, ForceSoftConflict: body.ForceSoftConflict, OverrideReason: body.OverrideReason})
	if err != nil {
		respondAppointmentError(c, err, "assign_closer_failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "conflicts": conflicts})
}

func (h *AppointmentHandler) Reschedule(c *gin.Context) {
	id, ok := parseInt64Param(c, "id", "invalid_appointment_id")
	if !ok {
		return
	}
	var body struct {
		StartAt           string `json:"start_at" binding:"required"`
		EndAt             string `json:"end_at" binding:"required"`
		ExpectedVersion   int    `json:"expected_version"`
		ForceSoftConflict bool   `json:"force_soft_conflicts"`
		OverrideReason    string `json:"override_reason"`
		Reason            string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	startAt, endAt, ok := parseTimeRange(c, body.StartAt, body.EndAt)
	if !ok {
		return
	}
	actor, role, ok := actorIdentity(c)
	if !ok {
		return
	}
	conflicts, err := h.appointments.Reschedule(c.Request.Context(), &usecase.RescheduleAppointmentRequest{AppointmentID: id, ActorUserID: actor, ActorRole: role, StartAt: startAt, EndAt: endAt, ExpectedVersion: body.ExpectedVersion, ForceSoftConflict: body.ForceSoftConflict, OverrideReason: body.OverrideReason, Reason: body.Reason})
	if err != nil {
		respondAppointmentError(c, err, "reschedule_appointment_failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "conflicts": conflicts})
}

func (h *AppointmentHandler) Cancel(c *gin.Context) { h.action(c, false) }
func (h *AppointmentHandler) NoShow(c *gin.Context) { h.action(c, true) }

func (h *AppointmentHandler) ConfirmArrival(c *gin.Context) {
	id, ok := parseInt64Param(c, "id", "invalid_appointment_id")
	if !ok {
		return
	}
	var body struct {
		ExpectedVersion int    `json:"expected_version"`
		Confirmation    string `json:"confirmation"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	actor, role, ok := actorIdentity(c)
	if !ok {
		return
	}
	if err := h.appointments.ConfirmArrival(c.Request.Context(), &usecase.ConfirmArrivalRequest{AppointmentID: id, ActorUserID: actor, ActorRole: role, ExpectedVersion: body.ExpectedVersion, Confirmation: body.Confirmation}); err != nil {
		respondUseCaseError(c, err, "confirm_arrival_failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *AppointmentHandler) action(c *gin.Context, noShow bool) {
	id, ok := parseInt64Param(c, "id", "invalid_appointment_id")
	if !ok {
		return
	}
	var body struct {
		ExpectedVersion int    `json:"expected_version"`
		Reason          string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	actor, role, ok := actorIdentity(c)
	if !ok {
		return
	}
	req := &usecase.AppointmentActionRequest{AppointmentID: id, ActorUserID: actor, ActorRole: role, ExpectedVersion: body.ExpectedVersion, Reason: body.Reason}
	var err error
	if noShow {
		err = h.appointments.NoShow(c.Request.Context(), req)
	} else {
		err = h.appointments.Cancel(c.Request.Context(), req)
	}
	if err != nil {
		respondUseCaseError(c, err, "appointment_action_failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *AppointmentHandler) Conflicts(c *gin.Context) {
	closer := strings.TrimSpace(c.Query("closer_userid"))
	startAt, endAt, ok := parseTimeRange(c, c.Query("start_at"), c.Query("end_at"))
	if !ok {
		return
	}
	excludeID, _ := strconv.ParseInt(c.Query("exclude_id"), 10, 64)
	items, err := h.appointments.DetectConflicts(c.Request.Context(), closer, startAt, endAt, c.Query("campus"), excludeID)
	if err != nil {
		respondUseCaseError(c, err, "detect_appointment_conflicts_failed")
		return
	}
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.AppointmentID)
	}
	RecordAppointmentReadIDs(c, ids...)
	c.JSON(http.StatusOK, gin.H{"conflicts": items})
}

func (h *AppointmentHandler) CreateAvailability(c *gin.Context) {
	var body struct {
		CloserUserID string `json:"closer_userid" binding:"required"`
		StartAt      string `json:"start_at" binding:"required"`
		EndAt        string `json:"end_at" binding:"required"`
		Campus       string `json:"campus"`
		Kind         string `json:"kind" binding:"required"`
		Reason       string `json:"reason"`
		Capacity     int    `json:"capacity"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	startAt, endAt, ok := parseTimeRange(c, body.StartAt, body.EndAt)
	if !ok {
		return
	}
	actor, role, ok := actorIdentity(c)
	if !ok {
		return
	}
	item := &types.CloserAvailability{CloserUserID: body.CloserUserID, StartAt: startAt, EndAt: endAt, Campus: body.Campus, Capacity: body.Capacity, Kind: body.Kind, Reason: body.Reason}
	if err := h.availability.Create(c.Request.Context(), item, actor, role); err != nil {
		respondUseCaseError(c, err, "create_availability_failed")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"availability": item})
}

func (h *AppointmentHandler) ListAvailability(c *gin.Context) {
	actor, role, ok := actorIdentity(c)
	if !ok {
		return
	}
	startAt, endAt, ok := parseOptionalRange(c)
	if !ok {
		return
	}
	items, err := h.availability.List(c.Request.Context(), c.Query("closer_userid"), startAt, endAt, actor, role)
	if err != nil {
		respondUseCaseError(c, err, "list_availability_failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"availability": items})
}

func (h *AppointmentHandler) DeleteAvailability(c *gin.Context) {
	id, ok := parseInt64Param(c, "id", "invalid_availability_id")
	if !ok {
		return
	}
	actor, role, ok := actorIdentity(c)
	if !ok {
		return
	}
	if err := h.availability.Delete(c.Request.Context(), id, actor, role); err != nil {
		respondUseCaseError(c, err, "delete_availability_failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func respondAppointmentError(c *gin.Context, err error, fallback string) {
	var conflict *usecase.AppointmentConflictError
	if errors.As(err, &conflict) {
		status := http.StatusConflict
		code := "appointment_soft_conflict"
		if errors.Is(err, types.ErrAppointmentHardConflict) {
			code = "appointment_hard_conflict"
		}
		c.JSON(status, gin.H{"error": code, "conflicts": conflict.Conflicts})
		return
	}
	respondUseCaseError(c, err, fallback)
}

func actorIdentity(c *gin.Context) (string, string, bool) {
	userID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return "", "", false
	}
	role, err := GetRole(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return "", "", false
	}
	return userID, role, true
}

func parseInt64Param(c *gin.Context, name, code string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": code})
		return 0, false
	}
	return id, true
}

func parseTimeRange(c *gin.Context, startRaw, endRaw string) (time.Time, time.Time, bool) {
	startAt, err := time.Parse(time.RFC3339, startRaw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_start_at"})
		return time.Time{}, time.Time{}, false
	}
	endAt, err := time.Parse(time.RFC3339, endRaw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_end_at"})
		return time.Time{}, time.Time{}, false
	}
	return startAt, endAt, true
}

func parseOptionalRange(c *gin.Context) (time.Time, time.Time, bool) {
	startRaw, endRaw := c.Query("start_at"), c.Query("end_at")
	if startRaw == "" && endRaw == "" {
		now := time.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -7)
		return start, start.AddDate(0, 0, 21), true
	}
	return parseTimeRange(c, startRaw, endRaw)
}
