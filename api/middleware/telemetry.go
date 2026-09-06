package middleware

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/internal/storage"
)

// TelemetryMiddleware records API performance, status codes, and request traffic per database.
func TelemetryMiddleware(metaDB *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/api/v1/databases") {
			return
		}

		// Don't record telemetry of the analytics endpoint itself to prevent infinite feedback loops
		if strings.HasSuffix(path, "/analytics") {
			return
		}

		dbName := c.Param("db_name")
		if dbName == "" {
			parts := strings.Split(strings.TrimPrefix(path, "/api/v1/databases/"), "/")
			if len(parts) > 0 && parts[0] != "" {
				dbName = parts[0]
			}
		}

		if dbName == "" {
			return
		}

		elapsed := time.Since(start).Milliseconds()
		status := c.Writer.Status()
		method := c.Request.Method
		endpoint := c.FullPath()
		if endpoint == "" {
			endpoint = path
		}

		go func(db string, ep, m string, s int, l int64) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = storage.RecordTelemetry(ctx, metaDB, db, ep, m, s, l)
		}(dbName, endpoint, method, status, elapsed)
	}
}
