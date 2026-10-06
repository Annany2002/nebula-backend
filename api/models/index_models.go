package models

import "github.com/Annany2002/nebula-backend/internal/domain"

// CreateIndexRequest defines a named index over one or more existing table columns.
type CreateIndexRequest struct {
	Name      string   `json:"name" binding:"required"`
	TableName string   `json:"table_name" binding:"required"`
	Columns   []string `json:"columns" binding:"required,min=1,max=64"`
	Unique    bool     `json:"unique"`
}

// CreateIndexResponse includes the index metadata returned by the objects catalog.
type CreateIndexResponse struct {
	Message string           `json:"message"`
	DBName  string           `json:"db_name"`
	Index   domain.IndexInfo `json:"index"`
}

// DropIndexResponse acknowledges removal of a custom index without deleting records.
type DropIndexResponse struct {
	Message   string `json:"message"`
	DBName    string `json:"db_name"`
	IndexName string `json:"index_name"`
}
