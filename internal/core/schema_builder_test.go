package core

import (
	"strings"
	"testing"

	"github.com/Annany2002/nebula-backend/api/models"
)

func TestBuildCreateTableSQL(t *testing.T) {
	t.Run("Valid simple table", func(t *testing.T) {
		req := &models.CreateSchemaRequest{
			TableName: "users",
			Columns: []models.ColumnDefinition{
				{Name: "username", Type: "TEXT"},
				{Name: "age", Type: "INTEGER"},
			},
		}

		sql, err := BuildCreateTableSQL(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS users") {
			t.Errorf("missing CREATE TABLE: %s", sql)
		}
		if !strings.Contains(sql, "id INTEGER PRIMARY KEY AUTOINCREMENT") {
			t.Errorf("missing id primary key: %s", sql)
		}
		if !strings.Contains(sql, "username TEXT") || !strings.Contains(sql, "age INTEGER") {
			t.Errorf("missing column defs: %s", sql)
		}
		if !strings.Contains(sql, "created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP") {
			t.Errorf("missing created_at: %s", sql)
		}
	})

	t.Run("Valid table with column-level foreign key", func(t *testing.T) {
		req := &models.CreateSchemaRequest{
			TableName: "orders",
			Columns: []models.ColumnDefinition{
				{
					Name: "user_id",
					Type: "INTEGER",
					ForeignKey: &models.ForeignKeyDefinition{
						TargetTable:  "users",
						TargetColumn: "id",
						OnDelete:     "CASCADE",
						OnUpdate:     "NO ACTION",
					},
				},
				{Name: "amount", Type: "REAL"},
			},
		}

		sql, err := BuildCreateTableSQL(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedFK := "FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE ON UPDATE NO ACTION"
		if !strings.Contains(sql, expectedFK) {
			t.Errorf("missing foreign key clause in SQL:\n%s\nexpected:\n%s", sql, expectedFK)
		}
	})

	t.Run("Valid table with table-level foreign key", func(t *testing.T) {
		req := &models.CreateSchemaRequest{
			TableName: "order_items",
			Columns: []models.ColumnDefinition{
				{Name: "order_id", Type: "INTEGER"},
				{Name: "item_id", Type: "INTEGER"},
			},
			ForeignKeys: []models.ForeignKeyTableConstraint{
				{
					Column:       "order_id",
					TargetTable:  "orders",
					TargetColumn: "id",
					OnDelete:     "SET NULL",
				},
			},
		}

		sql, err := BuildCreateTableSQL(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedFK := "FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE SET NULL"
		if !strings.Contains(sql, expectedFK) {
			t.Errorf("missing foreign key clause in SQL:\n%s\nexpected:\n%s", sql, expectedFK)
		}
	})

	t.Run("Invalid table name", func(t *testing.T) {
		req := &models.CreateSchemaRequest{
			TableName: "users; DROP TABLE users;",
			Columns: []models.ColumnDefinition{
				{Name: "test", Type: "TEXT"},
			},
		}
		_, err := BuildCreateTableSQL(req)
		if err == nil {
			t.Error("expected error for SQL injection / invalid table name")
		}
	})

	t.Run("Reserved column names", func(t *testing.T) {
		req := &models.CreateSchemaRequest{
			TableName: "test",
			Columns: []models.ColumnDefinition{
				{Name: "id", Type: "INTEGER"},
			},
		}
		_, err := BuildCreateTableSQL(req)
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("expected reserved column error for id, got: %v", err)
		}
	})

	t.Run("Duplicate column name", func(t *testing.T) {
		req := &models.CreateSchemaRequest{
			TableName: "test",
			Columns: []models.ColumnDefinition{
				{Name: "title", Type: "TEXT"},
				{Name: "TITLE", Type: "TEXT"},
			},
		}
		_, err := BuildCreateTableSQL(req)
		if err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Errorf("expected duplicate error, got: %v", err)
		}
	})

	t.Run("Invalid Foreign Key Target", func(t *testing.T) {
		req := &models.CreateSchemaRequest{
			TableName: "orders",
			Columns: []models.ColumnDefinition{
				{
					Name: "user_id",
					Type: "INTEGER",
					ForeignKey: &models.ForeignKeyDefinition{
						TargetTable:  "users; DROP TABLE users;",
						TargetColumn: "id",
					},
				},
			},
		}
		_, err := BuildCreateTableSQL(req)
		if err == nil {
			t.Error("expected error for invalid foreign key target table")
		}
	})

	t.Run("Invalid ON DELETE action", func(t *testing.T) {
		req := &models.CreateSchemaRequest{
			TableName: "orders",
			Columns: []models.ColumnDefinition{
				{
					Name: "user_id",
					Type: "INTEGER",
					ForeignKey: &models.ForeignKeyDefinition{
						TargetTable:  "users",
						TargetColumn: "id",
						OnDelete:     "INVALID_ACTION",
					},
				},
			},
		}
		_, err := BuildCreateTableSQL(req)
		if err == nil {
			t.Error("expected error for invalid ON DELETE action")
		}
	})
}
