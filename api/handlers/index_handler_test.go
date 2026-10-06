package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/domain"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

func TestIndexCreateAndDropLifecycle(t *testing.T) {
	f := newBackendFixture(t)
	id, db := f.database(t, "alpha")
	key, err := storage.StoreAPIKey(t.Context(), f.meta, "regression-owner", id)
	require.NoError(t, err)
	base := "/api/v1/databases/alpha/indexes"
	out := f.request("POST", base, "Bearer "+f.token, `{"name":"label_lookup","table_name":"ITEMS","columns":["LABEL"]}`)
	require.Equal(t, http.StatusCreated, out.Code, out.Body.String())
	var created models.CreateIndexResponse
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &created))
	require.Equal(t, "alpha", created.DBName)
	require.Equal(t, domain.IndexInfo{Name: "label_lookup", TableName: "items", Unique: false, SQL: `CREATE INDEX "label_lookup" ON "items" ("label")`}, created.Index)
	out = f.request("POST", "/api/v1/databases/alpha/sql", "Bearer "+f.token, `{"query":"EXPLAIN QUERY PLAN SELECT label FROM items WHERE label='fixture'"}`)
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	var plan domain.SQLQueryResult
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &plan))
	require.NotEmpty(t, plan.Rows)
	require.Contains(t, plan.Rows[0][3], "label_lookup")
	for _, payload := range []string{
		`{"name":"LABEL_LOOKUP","table_name":"items","columns":["label"]}`,
		`{"name":"label_lookup","table_name":"items","columns":["id"],"unique":true}`,
	} {
		out = f.request("POST", base, "ApiKey "+key, payload)
		require.Equal(t, http.StatusConflict, out.Code, out.Body.String())
	}
	out = f.request("POST", base, "ApiKey "+key, `{"name":"label_unique","table_name":"items","columns":["label"],"unique":true}`)
	require.Equal(t, http.StatusCreated, out.Code, out.Body.String())
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &created))
	require.True(t, created.Index.Unique)
	_, err = db.Exec(`INSERT INTO items VALUES(2,'fixture')`)
	require.Error(t, err, "unique index must enforce uniqueness")
	out = f.request("GET", "/api/v1/databases/alpha/objects", "ApiKey "+key, "")
	require.Equal(t, http.StatusOK, out.Code)
	var catalog domain.DatabaseObjects
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &catalog))
	require.Len(t, catalog.Indexes, 2)
	require.False(t, catalog.Indexes[0].Unique)
	out = f.request("DELETE", base+"/LABEL_UNIQUE", "ApiKey "+key, "")
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	var dropped models.DropIndexResponse
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &dropped))
	require.Equal(t, "label_unique", dropped.IndexName)
	require.Equal(t, "alpha", dropped.DBName)
	_, err = db.Exec(`INSERT INTO items VALUES(2,'fixture')`)
	require.NoError(t, err, "dropping the unique index must leave records editable")
	out = f.request("DELETE", base+"/label_unique", "Bearer "+f.token, "")
	require.Equal(t, http.StatusNotFound, out.Code)
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM items").Scan(&count))
	require.Equal(t, 2, count)
}

func TestIndexCreationSupportsCompositeGeneratedAndQuotedColumns(t *testing.T) {
	f := newBackendFixture(t)
	_, db := f.database(t, "alpha")
	table, column := `quoted"; DROP TABLE items;--`, `field"name`
	_, err := db.Exec("CREATE TABLE " + storage.QuoteIdentifier(table) + " (" + storage.QuoteIdentifier(column) + " TEXT, other TEXT, doubled TEXT GENERATED ALWAYS AS (other||other) VIRTUAL)")
	require.NoError(t, err)
	out := f.request("POST", "/api/v1/databases/alpha/indexes", "Bearer "+f.token, fixtureJSON(t, models.CreateIndexRequest{Name: "select", TableName: table, Columns: []string{column, "doubled"}, Unique: true}))
	require.Equal(t, http.StatusCreated, out.Code, out.Body.String())
	var result models.CreateIndexResponse
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &result))
	require.Equal(t, table, result.Index.TableName)
	var sql string
	require.NoError(t, db.QueryRow("SELECT sql FROM sqlite_master WHERE name='select'").Scan(&sql))
	require.Equal(t, result.Index.SQL, sql)
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM items").Scan(new(int)))
	legacyName := `legacy"; DROP TABLE items;--`
	_, err = db.Exec("CREATE INDEX " + storage.QuoteIdentifier(legacyName) + " ON items(label)")
	require.NoError(t, err)
	out = f.request("DELETE", "/api/v1/databases/alpha/indexes/"+url.PathEscape(legacyName), "Bearer "+f.token, "")
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM items").Scan(new(int)))
}

func TestUniqueIndexRejectsExistingDuplicatesWithoutLeavingAnIndex(t *testing.T) {
	f := newBackendFixture(t)
	_, db := f.database(t, "alpha")
	_, err := db.Exec(`INSERT INTO items VALUES(2,'fixture')`)
	require.NoError(t, err)
	out := f.request("POST", "/api/v1/databases/alpha/indexes", "Bearer "+f.token, `{"name":"unique_label","table_name":"items","columns":["label"],"unique":true}`)
	require.Equal(t, http.StatusConflict, out.Code, out.Body.String())
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='unique_label'").Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM items").Scan(&count))
	require.Equal(t, 2, count)
}

func TestIndexRequestValidationAndProtectedObjects(t *testing.T) {
	f := newBackendFixture(t)
	_, db := f.database(t, "alpha")
	_, err := db.Exec(`CREATE TABLE _nebula_metadata (value TEXT); CREATE INDEX metadata_lookup ON _nebula_metadata(value); CREATE TABLE constraint_table (value TEXT UNIQUE); CREATE VIEW item_view AS SELECT * FROM items; CREATE VIRTUAL TABLE search_items USING fts4(label); CREATE INDEX shadow_lookup ON search_items_content(docid);`)
	require.NoError(t, err)
	base := "/api/v1/databases/alpha/indexes"
	for _, payload := range []string{
		`{}`, `null`, `{`,
		`{"name":"lookup","table_name":"items","columns":["label"],"where":"label IS NOT NULL"}`,
		`{"name":"lookup","table_name":"items","columns":["label"],"collation":"NOCASE"}`,
		`{"name":"lookup","table_name":"items","columns":["label"]} {}`,
		`{"name":"x","table_name":"items","columns":[]}`,
		`{"name":"x","table_name":"items","columns":["label"],"unique":"true"}`,
		`{"name":"bad;DROP","table_name":"items","columns":["label"]}`,
		`{"name":"sqlite_hidden","table_name":"items","columns":["label"]}`,
		`{"name":"_NEBULA_hidden","table_name":"items","columns":["label"]}`,
		`{"name":"lookup","table_name":"_NEBULA_metadata","columns":["value"]}`,
		`{"name":"lookup","table_name":"items","columns":["label","LABEL"]}`,
		`{"name":"lookup","table_name":"items","columns":["missing"]}`,
		`{"name":"lookup","table_name":"items","columns":["lower(label)"]}`,
		`{"name":"lookup","table_name":"items","columns":["label\u0000"]}`,
		fixtureJSON(t, models.CreateIndexRequest{Name: strings.Repeat("x", 65), TableName: "items", Columns: []string{"label"}}),
		fixtureJSON(t, models.CreateIndexRequest{Name: "lookup", TableName: "items", Columns: strings.Split(strings.Repeat("label,", 64)+"label", ",")}),
	} {
		out := f.request("POST", base, "Bearer "+f.token, payload)
		require.Equal(t, http.StatusBadRequest, out.Code, "payload %s: %s", payload, out.Body.String())
	}
	for _, table := range []string{"search_items", "search_items_content"} {
		out := f.request("POST", base, "Bearer "+f.token, fixtureJSON(t, models.CreateIndexRequest{Name: "lookup", TableName: table, Columns: []string{"label"}}))
		require.Equal(t, http.StatusBadRequest, out.Code, out.Body.String())
	}
	for _, table := range []string{"missing", "item_view"} {
		out := f.request("POST", base, "Bearer "+f.token, fixtureJSON(t, models.CreateIndexRequest{Name: "lookup", TableName: table, Columns: []string{"label"}}))
		require.Equal(t, http.StatusNotFound, out.Code, out.Body.String())
	}
	out := f.request("POST", base, "Bearer "+f.token, `{"name":"items","table_name":"items","columns":["label"]}`)
	require.Equal(t, http.StatusConflict, out.Code)
	for _, name := range []string{"sqlite_autoindex_constraint_table_1", "_nebula_hidden", "metadata_lookup", "shadow_lookup"} {
		out = f.request("DELETE", base+"/"+name, "Bearer "+f.token, "")
		require.Equal(t, http.StatusBadRequest, out.Code, out.Body.String())
	}
	out = f.request("DELETE", base+"/items", "Bearer "+f.token, "")
	require.Equal(t, http.StatusNotFound, out.Code, out.Body.String())
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name IN ('metadata_lookup','sqlite_autoindex_constraint_table_1')").Scan(&count))
	require.Equal(t, 2, count)
}

func TestIndexEndpointsEnforceDatabaseAndOwnerScopes(t *testing.T) {
	f := newBackendFixture(t)
	id, db := f.database(t, "alpha")
	_, _ = f.database(t, "beta")
	key, err := storage.StoreAPIKey(t.Context(), f.meta, "regression-owner", id)
	require.NoError(t, err)
	body := `{"name":"lookup","table_name":"items","columns":["label"]}`
	for _, method := range []string{"POST", "DELETE"} {
		path := "/api/v1/databases/beta/indexes"
		if method == "DELETE" {
			path += "/lookup"
		}
		out := f.request(method, path, "ApiKey "+key, body)
		require.Equal(t, http.StatusForbidden, out.Code, out.Body.String())
		out = f.request(method, path, "", body)
		require.Equal(t, http.StatusUnauthorized, out.Code)
		out = f.request(method, path, "Bearer invalid.jwt", body)
		require.Equal(t, http.StatusUnauthorized, out.Code)
	}
	out := f.request("POST", "/api/v1/databases/missing/indexes", "Bearer "+f.token, body)
	require.Equal(t, http.StatusNotFound, out.Code)
	out = f.request("POST", "/api/v1/databases/bad-name/indexes", "Bearer "+f.token, body)
	require.Equal(t, http.StatusBadRequest, out.Code)
	_, err = storage.CreateUser(t.Context(), f.meta, "second-owner", "seconduser", "second@example.test", "hash")
	require.NoError(t, err)
	require.NoError(t, storage.RegisterDatabase(t.Context(), f.meta, "second-owner", "alpha", filepath.Join(t.TempDir(), "second.db")))
	otherPath, err := storage.FindDatabasePath(t.Context(), f.meta, "second-owner", "alpha")
	require.NoError(t, err)
	otherDB, err := storage.ConnectUserDB(t.Context(), otherPath)
	require.NoError(t, err)
	defer otherDB.Close()
	_, err = otherDB.Exec("CREATE TABLE items (label TEXT)")
	require.NoError(t, err)
	token, err := auth.GenerateJWT("second-owner", f.cfg.JWTSecret, f.cfg.JWTExpiration)
	require.NoError(t, err)
	out = f.request("POST", "/api/v1/databases/alpha/indexes", "Bearer "+token, body)
	require.Equal(t, http.StatusCreated, out.Code, out.Body.String())
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='lookup'").Scan(&count))
	require.Zero(t, count)
	out = f.request("DELETE", "/api/v1/databases/alpha/indexes/lookup", "ApiKey "+key, "")
	require.Equal(t, http.StatusNotFound, out.Code)
	require.NoError(t, otherDB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='lookup'").Scan(&count))
	require.Equal(t, 1, count)
}
