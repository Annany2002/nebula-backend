// internal/core/validation_test.go
package core

import (
	"strings"
	"testing"
)

func TestIsValidIdentifier(t *testing.T) {
	testCases := []struct {
		name    string
		input   string
		want    bool
		comment string
	}{
		{"valid simple", "my_table", true, ""},
		{"valid with numbers", "table_123", true, ""},
		{"valid uppercase", "MY_TABLE", true, ""},
		{"valid underscore start", "_table", true, ""}, // SQLite allows this
		{"valid underscore end", "table_", true, ""},
		{"valid number start", "123table", true, ""}, // Relaxed validation allows this, adjust regex if needed stricter
		{"valid short", "a", true, ""},
		{"valid long (64 chars)", strings.Repeat("a", 64), true, ""},
		{"invalid empty", "", false, "empty string"},
		{"invalid space", "my table", false, "contains space"},
		{"invalid hyphen", "my-table", false, "contains hyphen"},
		{"invalid special char", "table$", false, "contains dollar sign"},
		{"invalid path separator", "table/name", false, "contains path separator"},
		{"invalid too long", strings.Repeat("a", 65), false, "exceeds 64 chars"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsValidIdentifier(tc.input)
			if got != tc.want {
				t.Errorf("IsValidIdentifier(%q) = %v; want %v. %s", tc.input, got, tc.want, tc.comment)
			}
		})
	}
}

func TestNormalizeAndValidateType(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		wantType string
		wantOk   bool
		comment  string
	}{
		{"valid TEXT lower", "text", "TEXT", true, ""},
		{"valid TEXT upper", "TEXT", "TEXT", true, ""},
		{"valid TEXT mixed", "TeXt", "TEXT", true, ""},
		{"valid INTEGER", "integer", "INTEGER", true, ""},
		{"valid REAL", "real", "REAL", true, ""},
		{"valid BLOB", "blob", "BLOB", true, ""},
		{"valid BOOLEAN", "boolean", "BOOLEAN", true, ""},
		{"invalid type", "VARCHAR", "", false, "unsupported type"},
		{"invalid empty", "", "", false, "empty string"},
		{"invalid special chars", "TEXT$", "", false, "contains special char"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotType, gotOk := NormalizeAndValidateType(tc.input)
			if gotOk != tc.wantOk {
				t.Errorf("NormalizeAndValidateType(%q): gotOk = %v; wantOk %v. %s", tc.input, gotOk, tc.wantOk, tc.comment)
			}
			if gotType != tc.wantType {
				t.Errorf("NormalizeAndValidateType(%q): gotType = %q; wantType %q. %s", tc.input, gotType, tc.wantType, tc.comment)
			}
		})
	}
}
