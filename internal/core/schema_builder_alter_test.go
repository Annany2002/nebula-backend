package core

import (
	"strings"
	"testing"

	"github.com/Annany2002/nebula-backend/api/models"
)

func TestBuildAddColumnSQL(t *testing.T) {
	t.Run("valid basic add column", func(t *testing.T) {
		col := &models.AlterColumnDefinition{
			Name: "bio",
			Type: "TEXT",
		}
		sql, err := BuildAddColumnSQL("users", col)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "ALTER TABLE users ADD COLUMN bio TEXT;"
		if sql != expected {
			t.Errorf("got %q, want %q", sql, expected)
		}
	})

	t.Run("valid add column with default and not null", func(t *testing.T) {
		defaultVal := "'active'"
		col := &models.AlterColumnDefinition{
			Name:         "status",
			Type:         "TEXT",
			DefaultValue: &defaultVal,
			NotNull:      true,
		}
		sql, err := BuildAddColumnSQL("users", col)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active';"
		if sql != expected {
			t.Errorf("got %q, want %q", sql, expected)
		}
	})

	t.Run("reject not null without default", func(t *testing.T) {
		col := &models.AlterColumnDefinition{
			Name:    "status",
			Type:    "TEXT",
			NotNull: true,
		}
		_, err := BuildAddColumnSQL("users", col)
		if err == nil || !strings.Contains(err.Error(), "no DEFAULT value") {
			t.Errorf("expected error about missing DEFAULT, got %v", err)
		}
	})

	t.Run("reject reserved column id", func(t *testing.T) {
		col := &models.AlterColumnDefinition{
			Name: "id",
			Type: "INTEGER",
		}
		_, err := BuildAddColumnSQL("users", col)
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("expected error about reserved column id, got %v", err)
		}
	})

	t.Run("reject reserved column created_at", func(t *testing.T) {
		col := &models.AlterColumnDefinition{
			Name: "created_at",
			Type: "TEXT",
		}
		_, err := BuildAddColumnSQL("users", col)
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("expected error about reserved column created_at, got %v", err)
		}
	})
}

func TestBuildDropColumnSQL(t *testing.T) {
	t.Run("valid drop column", func(t *testing.T) {
		sql, err := BuildDropColumnSQL("users", "bio")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "ALTER TABLE users DROP COLUMN bio;"
		if sql != expected {
			t.Errorf("got %q, want %q", sql, expected)
		}
	})

	t.Run("reject dropping id", func(t *testing.T) {
		_, err := BuildDropColumnSQL("users", "id")
		if err == nil || !strings.Contains(err.Error(), "cannot drop reserved") {
			t.Errorf("expected error dropping id, got %v", err)
		}
	})

	t.Run("reject dropping created_at", func(t *testing.T) {
		_, err := BuildDropColumnSQL("users", "created_at")
		if err == nil || !strings.Contains(err.Error(), "cannot drop reserved") {
			t.Errorf("expected error dropping created_at, got %v", err)
		}
	})
}

func TestBuildRenameColumnSQL(t *testing.T) {
	t.Run("valid rename column", func(t *testing.T) {
		sql, err := BuildRenameColumnSQL("users", "bio", "about")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "ALTER TABLE users RENAME COLUMN bio TO about;"
		if sql != expected {
			t.Errorf("got %q, want %q", sql, expected)
		}
	})

	t.Run("reject renaming id", func(t *testing.T) {
		_, err := BuildRenameColumnSQL("users", "id", "user_id")
		if err == nil || !strings.Contains(err.Error(), "cannot rename reserved") {
			t.Errorf("expected error renaming id, got %v", err)
		}
	})

	t.Run("reject renaming to same name", func(t *testing.T) {
		_, err := BuildRenameColumnSQL("users", "bio", "bio")
		if err == nil || !strings.Contains(err.Error(), "different") {
			t.Errorf("expected error renaming to same name, got %v", err)
		}
	})
}

func TestBuildRenameTableSQL(t *testing.T) {
	t.Run("valid rename table", func(t *testing.T) {
		sql, err := BuildRenameTableSQL("old_users", "new_users")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "ALTER TABLE old_users RENAME TO new_users;"
		if sql != expected {
			t.Errorf("got %q, want %q", sql, expected)
		}
	})

	t.Run("reject renaming to same table name", func(t *testing.T) {
		_, err := BuildRenameTableSQL("users", "users")
		if err == nil || !strings.Contains(err.Error(), "different") {
			t.Errorf("expected error renaming to same table name, got %v", err)
		}
	})
}

func TestBuildAlterTableStatements(t *testing.T) {
	t.Run("batch operations", func(t *testing.T) {
		req := &models.AlterTableRequest{
			Operations: []models.AlterTableOperation{
				{
					Action: "add_column",
					Column: &models.AlterColumnDefinition{Name: "role", Type: "TEXT"},
				},
				{
					Action:     "rename_column",
					OldName:    "notes",
					NewName:    "remarks",
				},
				{
					Action:     "drop_column",
					ColumnName: "legacy_col",
				},
			},
		}

		stmts, finalTable, err := BuildAlterTableStatements("accounts", req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(stmts) != 3 {
			t.Fatalf("expected 3 statements, got %d", len(stmts))
		}
		if finalTable != "accounts" {
			t.Errorf("got finalTable %s, want accounts", finalTable)
		}
	})
}
