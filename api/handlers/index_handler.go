package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

// CreateIndex creates a standard or unique index within the authenticated database scope.
func (h *TableHandler) CreateIndex(c *gin.Context) {
	var req models.CreateIndexRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		_ = c.Error(fmt.Errorf("%w: provide name, table_name, 1–64 columns and an optional boolean unique field", auth.ErrBadRequest))
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		_ = c.Error(fmt.Errorf("%w: provide exactly one index definition", auth.ErrBadRequest))
		return
	}
	if err := binding.Validator.ValidateStruct(&req); err != nil {
		_ = c.Error(fmt.Errorf("%w: name, table_name and 1–64 columns are required", auth.ErrBadRequest))
		return
	}
	db, dbName, err := h.checkScopeAndGetUserDB(c)
	if err != nil {
		_ = c.Error(err)
		return
	}
	defer db.Close()
	index, err := storage.CreateIndex(c.Request.Context(), db, req.Name, req.TableName, req.Columns, req.Unique)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, models.CreateIndexResponse{Message: "Index created successfully", DBName: dbName, Index: index})
}

// DropIndex deletes a custom index without deleting the indexed table or its records.
func (h *TableHandler) DropIndex(c *gin.Context) {
	db, dbName, err := h.checkScopeAndGetUserDB(c)
	if err != nil {
		_ = c.Error(err)
		return
	}
	defer db.Close()
	name, err := storage.DropIndex(c.Request.Context(), db, c.Param("index_name"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, models.DropIndexResponse{Message: "Index dropped successfully", DBName: dbName, IndexName: name})
}
