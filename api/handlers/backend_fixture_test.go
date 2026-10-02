package handlers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/api"
	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

type backendFixture struct {
	meta   *sql.DB
	cfg    *config.Config
	router *gin.Engine
	token  string
}

func newBackendFixture(t *testing.T) *backendFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	meta, cfg, cleanup := testDBSetup(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	_, err := storage.CreateUser(ctx, meta, "regression-owner", "regression", "regression@example.test", "fixture-password-hash")
	require.NoError(t, err)
	token, err := auth.GenerateJWT("regression-owner", cfg.JWTSecret, cfg.JWTExpiration)
	require.NoError(t, err)
	return &backendFixture{meta: meta, cfg: cfg, router: api.SetupRouter(meta, cfg), token: token}
}

func (f *backendFixture) database(t *testing.T, name string) (int64, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(f.cfg.MetadataDbDir, name+".db")
	require.NoError(t, storage.RegisterDatabase(ctx, f.meta, "regression-owner", name, path))
	db, err := storage.ConnectUserDB(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY, label TEXT); INSERT INTO items VALUES (1, 'fixture');")
	require.NoError(t, err)
	id, err := storage.FindDatabaseIDByNameAndUser(ctx, f.meta, "regression-owner", name)
	require.NoError(t, err)
	return id, db
}

func (f *backendFixture) request(method, path, authorization, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authorization)
	out := httptest.NewRecorder()
	f.router.ServeHTTP(out, req)
	return out
}

func fixtureJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}
