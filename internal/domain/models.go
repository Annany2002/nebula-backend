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
	RowCount  int64        `json:"rowCount"`
	Columns   []ColumnInfo `json:"columns"`
}

type TableSchemaMetaData struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	PrimaryKey bool   `json:"pk"`
}

// SQLQueryResult represents result of an executed SQL query
type SQLQueryResult struct {
	Columns      []string `json:"columns,omitempty"`
	Rows         [][]any  `json:"rows,omitempty"`
	RowCount     int64    `json:"rowCount"`
	RowsAffected int64    `json:"rowsAffected"`
	ExecutionMs  int64    `json:"executionMs"`
	Message      string   `json:"message,omitempty"`
}

// DatabaseDetailMetadata represents detailed single database information
type DatabaseDetailMetadata struct {
	DatabaseID   int64     `json:"databaseId"`
	UserID       string    `json:"userId"`
	DBName       string    `json:"dbName"`
	FilePath     string    `json:"filePath"`
	CreatedAt    time.Time `json:"createdAt"`
	Tables       int64     `json:"tables"`
	TotalRecords int64     `json:"totalRecords"`
	SizeBytes    int64     `json:"sizeBytes"`
	SizeDisplay  string    `json:"sizeDisplay"`
	APIKey       string    `json:"apiKey"`
}

// ServiceMetricBucket represents hourly or aggregate metric for a service category
type ServiceMetricBucket struct {
	Timestamp string `json:"timestamp"`
	Requests  int64  `json:"requests"`
	Warnings  int64  `json:"warnings"`
	Errors    int64  `json:"errors"`
}

// ServiceMetrics represents a service card metrics breakdown
type ServiceMetrics struct {
	Name     string                `json:"name"`     // "SQL Engine", "Records API", "Tables API", "Auth & Keys"
	Requests int64                 `json:"requests"` // Total requests
	Warnings int64                 `json:"warnings"` // 4xx status codes
	Errors   int64                 `json:"errors"`   // 5xx status codes
	History  []ServiceMetricBucket `json:"history"`  // Hourly breakdown for bar charts
}

// AdvisorIssue represents a real schema or security finding on SQLite
type AdvisorIssue struct {
	ID          string `json:"id"`
	Category    string `json:"category"` // "SECURITY", "PERFORMANCE", "SCHEMA"
	Severity    string `json:"severity"` // "CRITICAL", "WARNING", "INFO"
	Title       string `json:"title"`
	Description string `json:"description"`
	TableName   string `json:"tableName,omitempty"`
	Suggestion  string `json:"suggestion"`
}

// DatabaseAnalytics represents complete analytics and advisor report
type DatabaseAnalytics struct {
	TotalRequests int64            `json:"totalRequests"`
	SuccessRate   float64          `json:"successRate"`
	Timeframe     string           `json:"timeframe"` // "24h"
	Services      []ServiceMetrics `json:"services"`
	Advisor       []AdvisorIssue   `json:"advisor"`
}
