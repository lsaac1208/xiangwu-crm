package http

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/pkg/types"
)

type accessReadMetadata struct {
	IDs       []string
	Aggregate bool
}

func RecordLeadRead(c *gin.Context, leads ...*types.Lead) {
	ids := make([]int64, 0, len(leads))
	for _, lead := range leads {
		if lead != nil {
			ids = append(ids, lead.ID)
		}
	}
	RecordLeadReadIDs(c, ids...)
}
func RecordLeadReadIDs(c *gin.Context, ids ...int64) {
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			values = append(values, "lead:"+strconv.FormatInt(id, 10))
		}
	}
	c.Set("access_read_metadata", accessReadMetadata{IDs: values})
}
func RecordAggregateRead(c *gin.Context, ids ...int64) {
	RecordLeadReadIDs(c, ids...)
	value, _ := c.Get("access_read_metadata")
	meta := value.(accessReadMetadata)
	meta.Aggregate = true
	c.Set("access_read_metadata", meta)
}
func RecordAppointmentReadIDs(c *gin.Context, ids ...int64) {
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			values = append(values, "appointment:"+strconv.FormatInt(id, 10))
		}
	}
	c.Set("access_read_metadata", accessReadMetadata{IDs: values})
}
