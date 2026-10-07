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

func TestTriggerCreateFireAndDropLifecycle(t *testing.T) {
	for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
		t.Run(event, func(t *testing.T) {
			f := newBackendFixture(t)
			id, db := f.database(t, "alpha")
			key, err := storage.StoreAPIKey(t.Context(), f.meta, "regression-owner", id)
			require.NoError(t, err)
			_, err = db.Exec(`CREATE TABLE audit (action TEXT, label TEXT)`)
			require.NoError(t, err)
			row := "NEW"
			if event == "DELETE" {
				row = "OLD"
			}
			body := "INSERT INTO audit VALUES ('" + event + "', " + row + ".label);"
			definition := models.CreateTriggerRequest{Name: "audit_items", TableName: "ITEMS", Event: strings.ToLower(event), Body: body}
			base := "/api/v1/databases/alpha/triggers"
			out := f.request("POST", base, "Bearer "+f.token, fixtureJSON(t, definition))
			require.Equal(t, http.StatusCreated, out.Code, out.Body.String())
			var created models.CreateTriggerResponse
			require.NoError(t, json.Unmarshal(out.Body.Bytes(), &created))
			require.Equal(t, "alpha", created.DBName)
			require.Equal(t, domain.TriggerInfo{Name: "audit_items", TableName: "items", SQL: `CREATE TRIGGER "audit_items" AFTER ` + event + " ON \"items\"\nBEGIN\n" + body + "\nEND"}, created.Trigger)
			var count int
			require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM audit").Scan(&count))
			require.Zero(t, count, "creation must not execute trigger actions")
			out = f.request("GET", "/api/v1/databases/alpha/objects", "ApiKey "+key, "")
			require.Equal(t, http.StatusOK, out.Code, out.Body.String())
			var catalog domain.DatabaseObjects
			require.NoError(t, json.Unmarshal(out.Body.Bytes(), &catalog))
			require.Equal(t, []domain.TriggerInfo{created.Trigger}, catalog.Triggers)
			definition.Name = "AUDIT_ITEMS"
			definition.Body = "SELECT 1;"
			out = f.request("POST", base, "ApiKey "+key, fixtureJSON(t, definition))
			require.Equal(t, http.StatusConflict, out.Code, out.Body.String())
			var mutation string
			switch event {
			case "INSERT":
				mutation = "INSERT INTO items VALUES(2, 'changed')"
			case "UPDATE":
				mutation = "UPDATE items SET label='changed' WHERE id=1"
			case "DELETE":
				mutation = "DELETE FROM items WHERE id=1"
			}
			out = f.request("POST", "/api/v1/databases/alpha/sql", "ApiKey "+key, fixtureJSON(t, map[string]string{"query": mutation}))
			require.Equal(t, http.StatusOK, out.Code, out.Body.String())
			var action, label string
			require.NoError(t, db.QueryRow("SELECT action, label FROM audit").Scan(&action, &label))
			require.Equal(t, event, action)
			expected := "changed"
			if event == "DELETE" {
				expected = "fixture"
			}
			require.Equal(t, expected, label)
			out = f.request("DELETE", base+"/AUDIT_ITEMS", "ApiKey "+key, "")
			require.Equal(t, http.StatusOK, out.Code, out.Body.String())
			var dropped models.DropTriggerResponse
			require.NoError(t, json.Unmarshal(out.Body.Bytes(), &dropped))
			require.Equal(t, "audit_items", dropped.TriggerName)
			require.Equal(t, "alpha", dropped.DBName)
			out = f.request("DELETE", base+"/audit_items", "Bearer "+f.token, "")
			require.Equal(t, http.StatusNotFound, out.Code)
			_, err = db.Exec("INSERT INTO items VALUES (3, 'later'); UPDATE items SET label='later'; DELETE FROM items;")
			require.NoError(t, err)
			require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM audit").Scan(&count))
			require.Equal(t, 1, count, "dropping the trigger must stop future actions and preserve audit records")
		})
	}
}

func TestTriggerUpdateColumnsConditionsAndBeforeValidation(t *testing.T) {
	f := newBackendFixture(t)
	_, db := f.database(t, "alpha")
	_, err := db.Exec(`CREATE TABLE audit (old_label TEXT, new_label TEXT);`)
	require.NoError(t, err)
	definition := models.CreateTriggerRequest{Name: "track_label", TableName: "items", Event: "UPDATE", UpdateOf: []string{"LABEL"}, When: "NEW.label <> OLD.label", Body: "INSERT INTO audit VALUES (OLD.label, NEW.label); -- comment with END;"}
	base := "/api/v1/databases/alpha/triggers"
	out := f.request("POST", base, "Bearer "+f.token, fixtureJSON(t, definition))
	require.Equal(t, http.StatusCreated, out.Code, out.Body.String())
	_, err = db.Exec(`UPDATE items SET id=2; UPDATE items SET label=label; UPDATE items SET label='changed';`)
	require.NoError(t, err)
	var oldLabel, newLabel string
	require.NoError(t, db.QueryRow("SELECT old_label, new_label FROM audit").Scan(&oldLabel, &newLabel))
	require.Equal(t, "fixture", oldLabel)
	require.Equal(t, "changed", newLabel)
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM audit").Scan(&count))
	require.Equal(t, 1, count)
	definition = models.CreateTriggerRequest{Name: "validate_label", TableName: "items", Event: "INSERT", Timing: "before", When: "NEW.label IS NULL", Body: "SELECT RAISE(ABORT, 'label is required');"}
	out = f.request("POST", base, "Bearer "+f.token, fixtureJSON(t, definition))
	require.Equal(t, http.StatusCreated, out.Code, out.Body.String())
	_, err = db.Exec("INSERT INTO items VALUES(3, NULL)")
	require.ErrorContains(t, err, "label is required")
	_, err = db.Exec("INSERT INTO items VALUES(3, 'valid')")
	require.NoError(t, err)
}

func TestTriggerBodyCannotExecuteAnInjectedTail(t *testing.T) {
	f := newBackendFixture(t)
	_, db := f.database(t, "alpha")
	_, err := db.Exec("CREATE TABLE audit (label TEXT)")
	require.NoError(t, err)
	base := "/api/v1/databases/alpha/triggers"
	for _, body := range []string{
		"SELECT 1; END; DROP TABLE items; --",
		"SELECT 1; END; INSERT INTO audit VALUES ('injected'); --",
		"SELECT 1; END; COMMIT; DROP TABLE items; --",
		"SELECT 1; END; CREATE TRIGGER second AFTER INSERT ON items BEGIN SELECT 1; --",
		"SELECT 1; END; /* an ignored tail */",
		"SELECT 1; END", // No semicolon also closes the trigger before the generated END.
	} {
		out := f.request("POST", base, "Bearer "+f.token, fixtureJSON(t, models.CreateTriggerRequest{Name: "injected", TableName: "items", Event: "INSERT", Body: body}))
		require.Equal(t, http.StatusBadRequest, out.Code, "body %q: %s", body, out.Body.String())
		var count int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM items").Scan(&count))
		require.Equal(t, 1, count)
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM audit").Scan(&count))
		require.Zero(t, count)
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='trigger'").Scan(&count))
		require.Zero(t, count)
	}
	out := f.request("POST", base, "Bearer "+f.token, fixtureJSON(t, models.CreateTriggerRequest{Name: "injected", TableName: "items", Event: "INSERT", When: "1) BEGIN SELECT 1; END; DELETE FROM items; --", Body: "SELECT 1;"}))
	require.Equal(t, http.StatusBadRequest, out.Code, out.Body.String())
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM items").Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='trigger'").Scan(&count))
	require.Zero(t, count)
	// SQL strings, comments and CASE END expressions must not be mistaken for tails.
	definition := models.CreateTriggerRequest{Name: "valid_body", TableName: "items", Event: "INSERT", Body: "INSERT INTO audit VALUES (CASE WHEN NEW.label = 'END; --' THEN 'BEGIN' ELSE NEW.label END);\n/* END; */ SELECT 1;"}
	out = f.request("POST", base, "Bearer "+f.token, fixtureJSON(t, definition))
	require.Equal(t, http.StatusCreated, out.Code, out.Body.String())
	_, err = db.Exec("INSERT INTO items VALUES(2, 'END; --')")
	require.NoError(t, err)
	var value string
	require.NoError(t, db.QueryRow("SELECT label FROM audit").Scan(&value))
	require.Equal(t, "BEGIN", value)
}

func TestTriggerInvalidBodiesRollBackWithoutWritingRecords(t *testing.T) {
	f := newBackendFixture(t)
	_, db := f.database(t, "alpha")
	base := "/api/v1/databases/alpha/triggers"
	for _, body := range []string{
		"SELECT NEW.missing;", "SELECT OLD.id;", "INSERT INTO missing VALUES (NEW.id);", "SELECT readfile('/etc/passwd');", "SELECT 1", "", " \n ", "SELECT 1;\x00", "SELECT 1;" + strings.Repeat(" ", 64*1024), "ATTACH '/tmp/other.db' AS other;", "CREATE TABLE x(y);", "SELECT RAISE(ABORT);",
	} {
		definition := models.CreateTriggerRequest{Name: "bad_body", TableName: "items", Event: "INSERT", Body: body}
		out := f.request("POST", base, "Bearer "+f.token, fixtureJSON(t, definition))
		require.Equal(t, http.StatusBadRequest, out.Code, "body %q: %s", body[:min(len(body), 100)], out.Body.String())
	}

	for _, definition := range []models.CreateTriggerRequest{
		{Name: "invalid", TableName: "items", Event: "UPDATE", Body: "SELECT NEW.missing;"},
		{Name: "invalid", TableName: "items", Event: "DELETE", Body: "SELECT NEW.id;"},
		{Name: "invalid", TableName: "items", Event: "INSERT", When: "0", Body: "SELECT NEW.missing;"},
		{Name: "invalid", TableName: "items", Event: "DELETE", When: "0", Body: "INSERT INTO missing VALUES (OLD.id);"},
	} {
		out := f.request("POST", base, "Bearer "+f.token, fixtureJSON(t, definition))
		require.Equal(t, http.StatusBadRequest, out.Code, "definition %+v: %s", definition, out.Body.String())
	}
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='trigger'").Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM items").Scan(&count))
	require.Equal(t, 1, count)
}

func TestTriggerRequestValidationAndCompilationRollback(t *testing.T) {
	f := newBackendFixture(t)
	_, db := f.database(t, "alpha")
	_, err := db.Exec(`CREATE TABLE _nebula_metadata (value TEXT); CREATE VIEW item_view AS SELECT * FROM items; CREATE VIRTUAL TABLE search_items USING fts4(label); CREATE TABLE generated (base TEXT, derived TEXT GENERATED ALWAYS AS (base||base));`)
	require.NoError(t, err)
	base := "/api/v1/databases/alpha/triggers"
	good := models.CreateTriggerRequest{Name: "test_trigger", TableName: "items", Event: "INSERT", Body: "SELECT 1;"}
	invalid := []models.CreateTriggerRequest{
		{}, {Name: "x", TableName: "items", Body: "SELECT 1;"},
	}
	for _, name := range []string{"sqlite_hidden", "_NEBULA_hidden", "bad-name", strings.Repeat("x", 65)} {
		d := good
		d.Name = name
		invalid = append(invalid, d)
	}
	for _, table := range []string{"_NEBULA_metadata", "item_view", "search_items", "search_items_content", "items\x00"} {
		d := good
		d.TableName = table
		invalid = append(invalid, d)
	}
	for _, event := range []string{"UPSERT", "INSERT; DROP TABLE items;"} {
		d := good
		d.Event = event
		invalid = append(invalid, d)
	}
	for _, timing := range []string{"INSTEAD OF", "AFTER INSERT;"} {
		d := good
		d.Timing = timing
		invalid = append(invalid, d)
	}
	for _, when := range []string{"NEW.missing IS NULL", "OLD.id=1", "1\x00", strings.Repeat("1", 8193)} {
		d := good
		d.When = when
		invalid = append(invalid, d)
	}
	for _, columns := range [][]string{{"label"}, {"missing"}, {"label", "LABEL"}, {"label\x00"}, {""}, strings.Split(strings.Repeat("label,", 64)+"label", ",")} {
		d := good
		d.UpdateOf = columns
		invalid = append(invalid, d)
		d.Event = "UPDATE"
		if len(columns) != 1 || columns[0] != "label" {
			invalid = append(invalid, d)
		}
	}
	d := good
	d.TableName = "generated"
	d.Event = "UPDATE"
	d.UpdateOf = []string{"derived"}
	invalid = append(invalid, d)
	for _, definition := range invalid {
		out := f.request("POST", base, "Bearer "+f.token, fixtureJSON(t, definition))
		require.Equal(t, http.StatusBadRequest, out.Code, "definition %+v: %s", definition, out.Body.String())
	}
	for _, payload := range []string{"null", "{}", "{", `{"name":"x","table_name":"items","event":"INSERT","body":"SELECT 1;","extra":true}`, fixtureJSON(t, good) + " {}", `{"name":"x","table_name":"items","event":"INSERT","body":42}`, strings.Repeat(" ", 128*1024) + fixtureJSON(t, good)} {
		out := f.request("POST", base, "Bearer "+f.token, payload)
		require.Equal(t, http.StatusBadRequest, out.Code, out.Body.String())
	}
	out := f.request("POST", base, "Bearer "+f.token, fixtureJSON(t, models.CreateTriggerRequest{Name: "items", TableName: "items", Event: "INSERT", Body: "SELECT 1;"}))
	require.Equal(t, http.StatusConflict, out.Code, out.Body.String())
	good.TableName = "missing"
	out = f.request("POST", base, "Bearer "+f.token, fixtureJSON(t, good))
	require.Equal(t, http.StatusNotFound, out.Code)
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='trigger'").Scan(&count))
	require.Zero(t, count, "all failed creations must roll back")
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM items").Scan(&count))
	require.Equal(t, 1, count)
}

func TestTriggerQuotedTablesAndLegacyDropProtection(t *testing.T) {
	f := newBackendFixture(t)
	_, db := f.database(t, "alpha")
	table, column := `quoted"; DROP TABLE items;--`, `field"name`
	_, err := db.Exec("CREATE TABLE " + storage.QuoteIdentifier(table) + " (" + storage.QuoteIdentifier(column) + " TEXT); CREATE TABLE audit(value TEXT)")
	require.NoError(t, err)
	definition := models.CreateTriggerRequest{Name: "select", TableName: table, Event: "INSERT", Body: "INSERT INTO audit VALUES (NEW." + storage.QuoteIdentifier(column) + ");"}
	base := "/api/v1/databases/alpha/triggers"
	out := f.request("POST", base, "Bearer "+f.token, fixtureJSON(t, definition))
	require.Equal(t, http.StatusCreated, out.Code, out.Body.String())
	_, err = db.Exec("INSERT INTO " + storage.QuoteIdentifier(table) + " VALUES ('quoted')")
	require.NoError(t, err)
	var value string
	require.NoError(t, db.QueryRow("SELECT value FROM audit").Scan(&value))
	require.Equal(t, "quoted", value)
	legacy := `legacy"; DROP TABLE items;--`
	_, err = db.Exec("CREATE TRIGGER " + storage.QuoteIdentifier(legacy) + " AFTER INSERT ON items BEGIN SELECT 1; END; CREATE VIEW item_view AS SELECT * FROM items; CREATE TRIGGER view_trigger INSTEAD OF INSERT ON item_view BEGIN INSERT INTO items VALUES (NEW.id, NEW.label); END; CREATE TABLE _nebula_metadata(value TEXT); CREATE TRIGGER metadata_trigger AFTER INSERT ON _nebula_metadata BEGIN SELECT 1; END; CREATE TRIGGER _nebula_hidden AFTER INSERT ON items BEGIN SELECT 1; END; CREATE VIRTUAL TABLE search_items USING fts4(label); CREATE TRIGGER shadow_trigger AFTER INSERT ON search_items_content BEGIN SELECT 1; END;")
	require.NoError(t, err)
	for _, name := range []string{legacy, "view_trigger"} {
		out = f.request("DELETE", base+"/"+url.PathEscape(name), "Bearer "+f.token, "")
		require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	}
	for _, name := range []string{"metadata_trigger", "_nebula_hidden", "sqlite_hidden", "shadow_trigger"} {
		out = f.request("DELETE", base+"/"+name, "Bearer "+f.token, "")
		require.Equal(t, http.StatusBadRequest, out.Code, out.Body.String())
	}
	for _, name := range []string{"missing", "items", "view_trigger"} {
		out = f.request("DELETE", base+"/"+name, "Bearer "+f.token, "")
		require.Equal(t, http.StatusNotFound, out.Code, out.Body.String())
	}
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM items").Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name IN ('metadata_trigger', '_nebula_hidden', 'shadow_trigger')").Scan(&count))
	require.Equal(t, 3, count)
}

func TestTriggerEndpointsEnforceDatabaseAndOwnerScopes(t *testing.T) {
	f := newBackendFixture(t)
	id, db := f.database(t, "alpha")
	_, _ = f.database(t, "beta")
	key, err := storage.StoreAPIKey(t.Context(), f.meta, "regression-owner", id)
	require.NoError(t, err)
	definition := models.CreateTriggerRequest{Name: "custom", TableName: "items", Event: "INSERT", Body: "SELECT 1;"}
	body := fixtureJSON(t, definition)
	for _, method := range []string{"POST", "DELETE"} {
		path := "/api/v1/databases/beta/triggers"
		if method == "DELETE" {
			path += "/custom"
		}
		for _, header := range []string{"", "Bearer invalid.jwt", "ApiKey invalid"} {
			out := f.request(method, path, header, body)
			require.Equal(t, http.StatusUnauthorized, out.Code, out.Body.String())
		}
		out := f.request(method, path, "ApiKey "+key, body)
		require.Equal(t, http.StatusForbidden, out.Code, out.Body.String())
	}
	out := f.request("POST", "/api/v1/databases/missing/triggers", "Bearer "+f.token, body)
	require.Equal(t, http.StatusNotFound, out.Code)
	out = f.request("POST", "/api/v1/databases/bad-name/triggers", "Bearer "+f.token, body)
	require.Equal(t, http.StatusBadRequest, out.Code)
	_, err = storage.CreateUser(t.Context(), f.meta, "second-owner", "seconduser", "second@example.test", "hash")
	require.NoError(t, err)
	otherPath := filepath.Join(t.TempDir(), "second.db")
	require.NoError(t, storage.RegisterDatabase(t.Context(), f.meta, "second-owner", "alpha", otherPath))
	otherDB, err := storage.ConnectUserDB(t.Context(), otherPath)
	require.NoError(t, err)
	defer otherDB.Close()
	_, err = otherDB.Exec("CREATE TABLE items(label TEXT)")
	require.NoError(t, err)
	token, err := auth.GenerateJWT("second-owner", f.cfg.JWTSecret, f.cfg.JWTExpiration)
	require.NoError(t, err)
	out = f.request("POST", "/api/v1/databases/alpha/triggers", "Bearer "+token, body)
	require.Equal(t, http.StatusCreated, out.Code, out.Body.String())
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='trigger'").Scan(&count))
	require.Zero(t, count)
	out = f.request("DELETE", "/api/v1/databases/alpha/triggers/custom", "ApiKey "+key, "")
	require.Equal(t, http.StatusNotFound, out.Code)
	require.NoError(t, otherDB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='trigger'").Scan(&count))
	require.Equal(t, 1, count)
	// Scoped keys can create on their own database, independently of another owner's name.
	out = f.request("POST", "/api/v1/databases/alpha/triggers", "ApiKey "+key, body)
	require.Equal(t, http.StatusCreated, out.Code, out.Body.String())
	out = f.request("DELETE", "/api/v1/databases/alpha/triggers/custom", "Bearer "+token, "")
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='trigger'").Scan(&count))
	require.Equal(t, 1, count)
}
