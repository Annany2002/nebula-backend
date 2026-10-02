package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecordCRUDUsesTheActualPrimaryKey(t *testing.T) {
	for _, tc := range []struct{ name, definition, id, create string }{
		{"renamed integer key", "item_key INTEGER PRIMARY KEY", "7", `{"item_key":7,"label":"created"}`},
		{"text key", "item_key TEXT PRIMARY KEY", "item-seven", `{"item_key":"item-seven","label":"created"}`},
		{"text id", "id TEXT PRIMARY KEY", "item-seven", `{"id":"item-seven","label":"created"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBackendFixture(t)
			_, db := f.database(t, "alpha")
			_, err := db.Exec("CREATE TABLE custom (" + tc.definition + ",label TEXT)")
			require.NoError(t, err)
			authorization := "Bearer " + f.token
			base := "/api/v1/databases/alpha/tables/custom/records"
			out := f.request("POST", base, authorization, tc.create)
			require.Equal(t, http.StatusCreated, out.Code, out.Body.String())
			var created map[string]any
			require.NoError(t, json.Unmarshal(out.Body.Bytes(), &created))
			var input map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.create), &input))
			keyName := strings.Fields(tc.definition)[0]
			require.Equal(t, input[keyName], created["record_id"])
			out = f.request("GET", base+"/"+tc.id, authorization, "")
			require.Equal(t, http.StatusOK, out.Code, out.Body.String())
			require.Contains(t, out.Body.String(), "created")
			out = f.request("PUT", base+"/"+tc.id, authorization, `{"label":"updated"}`)
			require.Equal(t, http.StatusOK, out.Code, out.Body.String())
			out = f.request("GET", base+"/"+tc.id, authorization, "")
			require.Equal(t, http.StatusOK, out.Code, out.Body.String())
			require.Contains(t, out.Body.String(), "updated")
			out = f.request("DELETE", base+"/"+tc.id, authorization, "")
			require.Equal(t, http.StatusNoContent, out.Code, out.Body.String())
			out = f.request("GET", base+"/"+tc.id, authorization, "")
			require.Equal(t, http.StatusNotFound, out.Code, out.Body.String())
		})
	}
}

func TestSingleRecordRoutesRejectAmbiguousPrimaryKeys(t *testing.T) {
	for _, definition := range []string{"a INTEGER,b INTEGER,PRIMARY KEY(a,b)", "a INTEGER"} {
		f := newBackendFixture(t)
		_, db := f.database(t, "alpha")
		_, err := db.Exec("CREATE TABLE ambiguous (" + definition + ")")
		require.NoError(t, err)
		out := f.request("GET", "/api/v1/databases/alpha/tables/ambiguous/records/1", "Bearer "+f.token, "")
		require.Equal(t, http.StatusBadRequest, out.Code, out.Body.String())
	}
}
