package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/internal/domain"
)

func TestDatabaseObjectsReportsSQLiteIndexUniqueness(t *testing.T) {
	f := newBackendFixture(t)
	_, db := f.database(t, "objects")
	_, err := db.Exec(`
CREATE INDEX non_unique_label ON items(label);
CREATE INDEX commented_label ON items(label) /* UNIQUE is only a comment */;
CREATE UNIQUE INDEX label_lookup ON items(label);
CREATE TABLE "quoted table" ("unique label" TEXT UNIQUE);
CREATE INDEX "quoted index" ON "quoted table" ("unique label");
CREATE UNIQUE INDEX "quoted unique index" ON "quoted table" ("unique label");`)
	require.NoError(t, err)
	out := f.request("GET", "/api/v1/databases/objects/objects", "Bearer "+f.token, "")
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	var objects domain.DatabaseObjects
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &objects))
	require.Len(t, objects.Indexes, 5)
	want := map[string]bool{
		"non_unique_label":    false,
		"commented_label":     false,
		"label_lookup":        true,
		"quoted index":        false,
		"quoted unique index": true,
	}
	for _, index := range objects.Indexes {
		unique, ok := want[index.Name]
		require.True(t, ok, "unexpected index %q", index.Name)
		require.Equal(t, unique, index.Unique, "index %q", index.Name)
		require.NotEmpty(t, index.SQL)
	}
	require.Empty(t, objects.Triggers)
}
