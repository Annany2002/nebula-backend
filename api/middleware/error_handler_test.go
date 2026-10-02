package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

func TestErrorHandlerMapsWrappedClientErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{auth.ErrForbidden, http.StatusForbidden},
		{auth.ErrBadRequest, http.StatusBadRequest},
		{storage.ErrInvalidSortColumn, http.StatusBadRequest},
		{storage.ErrInvalidFieldColumn, http.StatusBadRequest},
	} {
		t.Run(tc.err.Error(), func(t *testing.T) {
			router := gin.New()
			router.Use(ErrorHandler())
			router.GET("/fixture", func(c *gin.Context) { _ = c.Error(fmt.Errorf("request rejected: %w", tc.err)) })
			out := httptest.NewRecorder()
			router.ServeHTTP(out, httptest.NewRequest("GET", "/fixture", http.NoBody))
			require.Equal(t, tc.status, out.Code, out.Body.String())
		})
	}
}
