package models

// ImportSQLiteResponse acknowledges a new database created from a standalone snapshot.
type ImportSQLiteResponse struct {
	Message   string `json:"message"`
	DBName    string `json:"db_name"`
	SizeBytes int64  `json:"size_bytes"`
}
