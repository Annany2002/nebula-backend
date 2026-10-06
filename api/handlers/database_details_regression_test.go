package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/internal/domain"
)

func TestDatabaseDetailsCountsQuotedTableNames(t *testing.T) {
	f := newBackendFixture(t)
	_, db := f.database(t, "details")
	_, err := db.Exec(`CREATE TABLE "quoted table" (label TEXT);
 INSERT INTO "quoted table" VALUES ('one'),('two');
 CREATE TABLE "select" (label TEXT);
 INSERT INTO "select" VALUES ('three');
 CREATE TABLE "quote""name" (label TEXT);
 INSERT INTO "quote""name" VALUES ('four');`)
	require.NoError(t, err)
	out := f.request("GET", "/api/v1/databases/details", "Bearer "+f.token, "")
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	var result struct {
		Database domain.DatabaseDetailMetadata `json:"database"`
	}
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &result))
	require.EqualValues(t, 4, result.Database.Tables)
	require.EqualValues(t, 5, result.Database.TotalRecords)
}
