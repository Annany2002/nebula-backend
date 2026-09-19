// api/models/database_models.go
package models

// --- Database/Schema Request Structs ---

// CreateDatabaseRequest defines the structure for creating a database registration
type CreateDatabaseRequest struct {
	DBName string `json:"db_name" binding:"required"`
}

// ForeignKeyDefinition represents a foreign key reference configuration
type ForeignKeyDefinition struct {
	TargetTable  string `json:"target_table" binding:"required"`
	TargetColumn string `json:"target_column" binding:"required"`
	OnDelete     string `json:"on_delete,omitempty"` // CASCADE, SET NULL, SET DEFAULT, RESTRICT, NO ACTION
	OnUpdate     string `json:"on_update,omitempty"` // CASCADE, SET NULL, SET DEFAULT, RESTRICT, NO ACTION
}

// ForeignKeyTableConstraint represents a table-level foreign key constraint
type ForeignKeyTableConstraint struct {
	Column       string `json:"column" binding:"required"`
	TargetTable  string `json:"target_table" binding:"required"`
	TargetColumn string `json:"target_column" binding:"required"`
	OnDelete     string `json:"on_delete,omitempty"`
	OnUpdate     string `json:"on_update,omitempty"`
}

// ColumnDefinition represents a single column in a table schema request
type ColumnDefinition struct {
	Name       string                `json:"name" binding:"required"`
	Type       string                `json:"type" binding:"required"` // e.g., "TEXT", "INTEGER", "REAL", "BLOB"
	ForeignKey *ForeignKeyDefinition `json:"foreign_key,omitempty"`
}

// CreateSchemaRequest defines the structure for the schema creation request body
type CreateSchemaRequest struct {
	TableName   string                      `json:"table_name" binding:"required"`
	Columns     []ColumnDefinition          `json:"columns" binding:"required_without=Schema"`
	Schema      []ColumnDefinition          `json:"schema" binding:"required_without=Columns"`
	ForeignKeys []ForeignKeyTableConstraint `json:"foreign_keys,omitempty"`
}

// CreateAPIKeyResponse returns the newly generated API key ONCE.
type CreateAPIKeyResponse struct {
	APIKey  string `json:"api_key"` // The full key (prefix + secret). Store securely!
	Message string `json:"message,omitempty"`
}

// AlterColumnDefinition represents the column definition when adding a column.
type AlterColumnDefinition struct {
	Name         string                `json:"name" binding:"required"`
	Type         string                `json:"type" binding:"required"`
	DefaultValue *string               `json:"default_value,omitempty"`
	NotNull      bool                  `json:"not_null,omitempty"`
	ForeignKey   *ForeignKeyDefinition `json:"foreign_key,omitempty"`
}

// AlterTableOperation represents a single alteration action.
type AlterTableOperation struct {
	Action       string                 `json:"action" binding:"required"` // add_column, drop_column, rename_column, rename_table
	Column       *AlterColumnDefinition `json:"column,omitempty"`          // for add_column
	ColumnName   string                 `json:"column_name,omitempty"`     // for drop_column
	OldName      string                 `json:"old_name,omitempty"`        // for rename_column
	NewName      string                 `json:"new_name,omitempty"`        // for rename_column
	NewTableName string                 `json:"new_table_name,omitempty"`  // for rename_table
}

// AlterTableRequest represents request payload for altering a table.
type AlterTableRequest struct {
	Action       string                 `json:"action,omitempty"`
	Column       *AlterColumnDefinition `json:"column,omitempty"`
	ColumnName   string                 `json:"column_name,omitempty"`
	OldName      string                 `json:"old_name,omitempty"`
	NewName      string                 `json:"new_name,omitempty"`
	NewTableName string                 `json:"new_table_name,omitempty"`
	Operations   []AlterTableOperation  `json:"operations,omitempty"`
}

// GetOperations normalizes single-action or batch operations into a slice.
func (r *AlterTableRequest) GetOperations() []AlterTableOperation {
	if len(r.Operations) > 0 {
		return r.Operations
	}
	if r.Action != "" {
		return []AlterTableOperation{
			{
				Action:       r.Action,
				Column:       r.Column,
				ColumnName:   r.ColumnName,
				OldName:      r.OldName,
				NewName:      r.NewName,
				NewTableName: r.NewTableName,
			},
		}
	}
	return nil
}
