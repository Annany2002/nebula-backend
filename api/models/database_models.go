// api/models/database_models.go
package models

// --- Database/Schema Request Structs ---

// CreateDatabaseRequest defines the structure for creating a database registration
type CreateDatabaseRequest struct {
	DBName string `json:"db_name" binding:"required"`
}

// ColumnDefinition represents a single column in a table schema request
type ColumnDefinition struct {
	Name string `json:"name" binding:"required"`
	Type string `json:"type" binding:"required"` // e.g., "TEXT", "INTEGER", "REAL", "BLOB"
}

// CreateSchemaRequest defines the structure for the schema creation request body
type CreateSchemaRequest struct {
	TableName string             `json:"table_name" binding:"required"`
	Columns   []ColumnDefinition `json:"columns" binding:"required,min=1,dive"`
}

// --- Record Response Structs ---
// (We can define specific response structs later if needed,
// for now handlers return map[string]interface{} or simple messages)

// Example: Simple success response with ID
// type CreateRecordResponse struct {
//     Message  string `json:"message"`
//     RecordID int64  `json:"record_id"`
// }
