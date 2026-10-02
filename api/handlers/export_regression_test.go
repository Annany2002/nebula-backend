package handlers_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSQLiteDownloadContainsCommittedWALData(t *testing.T) {
	f := newBackendFixture(t)
	_, db := f.database(t, "alpha")
	db.SetMaxOpenConns(1)
	_, err := db.Exec("PRAGMA wal_autocheckpoint=0; INSERT INTO items VALUES (2,'in-wal');")
	require.NoError(t, err)
	out := f.request("GET", "/api/v1/databases/alpha/export/sqlite", "Bearer "+f.token, "")
	require.Equal(t, http.StatusOK, out.Code)
	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	require.NoError(t, os.WriteFile(snapshot, out.Body.Bytes(), 0600))
	restored, err := sql.Open("sqlite3", snapshot)
	require.NoError(t, err)
	defer restored.Close()
	var label string
	require.NoError(t, restored.QueryRow("SELECT label FROM items WHERE id=2").Scan(&label))
	require.Equal(t, "in-wal", label)
	var integrity string
	require.NoError(t, restored.QueryRow("PRAGMA integrity_check").Scan(&integrity))
	require.Equal(t, "ok", integrity)
}

func TestSQLExportCanRestoreSQLiteTypesAndViews(t *testing.T) {
	f := newBackendFixture(t)
	_, db := f.database(t, "alpha")
	_, err := db.Exec(`CREATE TABLE export_types (id INTEGER PRIMARY KEY, bytes BLOB, text_value TEXT,created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);
 INSERT INTO export_types(id,bytes,text_value) VALUES (1,X'00FF','O''Brien'||char(0)||'suffix');
 CREATE VIEW export_view AS SELECT id,bytes FROM export_types;
 CREATE INDEX export_idx ON export_types(text_value);`)
	require.NoError(t, err)
	out := f.request("GET", "/api/v1/databases/alpha/export/sql", "Bearer "+f.token, "")
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	var dump struct {
		SQL string `json:"sql"`
	}
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &dump))
	restored, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "restored.db"))
	require.NoError(t, err)
	defer restored.Close()
	_, err = restored.Exec(dump.SQL)
	require.NoError(t, err)
	var blob, text, timestamp string
	require.NoError(t, restored.QueryRow("SELECT hex(bytes),hex(text_value),created_at FROM export_types WHERE id=1").Scan(&blob, &text, &timestamp))
	require.Equal(t, "00FF", blob)
	require.Equal(t, "4F27427269656E00737566666978", text)
	require.NotEmpty(t, timestamp)
	var count int
	require.NoError(t, restored.QueryRow("SELECT COUNT(*) FROM export_view").Scan(&count))
	require.Equal(t, 1, count)
}
