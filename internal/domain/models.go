// internal/domain/models.go
package domain

import "time"

// User defines the structure for user data in the DB
type UserMetadata struct {
	UserId       string    `json:"userId"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"password"`
	CreatedAt    time.Time `json:"createdAt"`
}

// DatabaseMetadata define the structure for user's databases
type DatabaseMetadata struct {
	DatabaseID int64     `json:"databaseId"`
	UserID     string    `json:"userId"`
	DBName     string    `json:"dbName"`
	FilePath   string    `json:"filePath"`
	CreatedAt  time.Time `json:"createdAt"`
	Tables     int64     `json:"tables"`
	APIKey     string    `json:"apiKey"`
}

// ColumnInfo represents the information for a single column.
type ColumnInfo struct {
	ColumnId string `json:"cid"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	NotNull  int    `json:"notnull"`
	Default  any    `json:"dflt_value"`
	PK       int    `json:"pk"`
}

// TableMetadata represents the information for a table, including its columns.
type TableMetadata struct {
	Type      string       `json:"type"`
	Name      string       `json:"name"`
	TableName string       `json:"tbl_name"`
	RootPage  string       `json:"rootpage"`
	Sql       string       `json:"sql"`
	CreatedAt time.Time    `json:"createdAt"`
	Columns   []ColumnInfo `json:"columns"`
}

type TableSchemaMetaData struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	PrimaryKey bool   `json:"pk"`
}
