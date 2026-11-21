package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/usecase"
)

type WorkbenchHandler struct{ workbench *usecase.GetWorkbenchUseCase }

func NewWorkbenchHandler(workbench *usecase.GetWorkbenchUseCase) *WorkbenchHandler {
	return &WorkbenchHandler{workbench: workbench}
}

func (h *WorkbenchHandler) Get(c *gin.Context) {
	actor, role, ok := actorIdentity(c)
	if !ok {
		return
	}
	result, err := h.workbench.Execute(c.Request.Context(), actor, role)
	if err != nil {
		respondInternalError(c, "get_workbench_failed", err)
		return
	}
	ids := make([]int64, 0, len(result.Tasks))
	for _, task := range result.Tasks {
		ids = append(ids, task.LeadID)
	}
	RecordLeadReadIDs(c, ids...)
	c.JSON(http.StatusOK, result)
}
