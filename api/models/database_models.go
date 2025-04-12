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

// CreateAPIKeyRequest defines the payload for requesting a new API key.
type CreateAPIKeyRequest struct {
	Label string `json:"label" binding:"required,max=100"` // User-provided name/description for the key
}

// CreateAPIKeyResponse returns the newly generated API key ONCE.
type CreateAPIKeyResponse struct {
	Label   string `json:"label"`
	APIKey  string `json:"api_key"` // The full key (prefix + secret). Store securely!
	Message string `json:"message,omitempty"`
}
