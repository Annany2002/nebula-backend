package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

// CreateTrigger creates a trigger within the authenticated database scope.
func (h *TableHandler) CreateTrigger(c *gin.Context) {
	var req models.CreateTriggerRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 128*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		_ = c.Error(fmt.Errorf("%w: provide a trigger definition within the 128 KiB request limit", auth.ErrBadRequest))
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		_ = c.Error(fmt.Errorf("%w: provide exactly one trigger definition", auth.ErrBadRequest))
		return
	}
	db, dbName, err := h.checkScopeAndGetUserDB(c)
	if err != nil {
		_ = c.Error(err)
		return
	}
	defer db.Close()
	trigger, err := storage.CreateTrigger(c.Request.Context(), db, &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, models.CreateTriggerResponse{Message: "Trigger created successfully", DBName: dbName, Trigger: trigger})
}

// DropTrigger removes a persistent custom trigger without modifying existing records.
func (h *TableHandler) DropTrigger(c *gin.Context) {
	db, dbName, err := h.checkScopeAndGetUserDB(c)
	if err != nil {
		_ = c.Error(err)
		return
	}
	defer db.Close()
	name, err := storage.DropTrigger(c.Request.Context(), db, c.Param("trigger_name"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, models.DropTriggerResponse{Message: "Trigger dropped successfully", DBName: dbName, TriggerName: name})
}
