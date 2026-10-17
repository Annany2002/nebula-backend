package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/config"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

func TestAdminMiddlewareFailsClosedOnMissingIdentityKeysAndStorageFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := storage.ConnectMetadataDB(&config.Config{MetadataDbDir: t.TempDir(), MetadataDbFile: "metadata.db"})
	require.NoError(t, err)
	defer db.Close()
	_, err = storage.CreateUser(context.Background(), db, "owner", "owner", "owner@example.test", "private")
	require.NoError(t, err)
	require.NoError(t, storage.SetPlatformAdmin(context.Background(), db, "owner", true))
	for _, scenario := range []struct {
		identity any
		key      bool
	}{
		{nil, false}, {7, false}, {"", false}, {"owner", true},
	} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("userId", scenario.identity)
			c.Set("isApiKey", scenario.key)
		})
		router.Use(RequirePlatformAdmin(db))
		router.GET("/", func(c *gin.Context) { c.Status(200) })
		out := httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest("GET", "/", http.NoBody))
		require.Equal(t, 403, out.Code)
	}
	require.NoError(t, db.Close())
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("userId", "owner") }, RequirePlatformAdmin(db))
	router.GET("/", func(c *gin.Context) { c.Status(200) })
	out := httptest.NewRecorder()
	router.ServeHTTP(out, httptest.NewRequest("GET", "/", http.NoBody))
	require.Equal(t, 503, out.Code)
	require.NotContains(t, out.Body.String(), "database is closed")
}

func TestAdminReadContextInheritsCancellationAndSetsBoundedDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	router := gin.New()
	router.Use(AdminRequestContext())
	router.GET("/", func(c *gin.Context) {
		deadline, ok := c.Request.Context().Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(3*time.Second), deadline, time.Second)
		cancel()
		require.ErrorIs(t, c.Request.Context().Err(), context.Canceled)
		c.Status(200)
	})
	request := httptest.NewRequest("GET", "/", http.NoBody).WithContext(parent)
	out := httptest.NewRecorder()
	router.ServeHTTP(out, request)
	require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	require.Contains(t, out.Header().Values("Vary"), "Authorization")
}
