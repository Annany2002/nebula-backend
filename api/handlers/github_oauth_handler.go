package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Annany2002/nebula-backend/api/middleware"
	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/internal/accounttoken"
	"github.com/Annany2002/nebula-backend/internal/auth"
	"github.com/Annany2002/nebula-backend/internal/githubauth"
	"github.com/Annany2002/nebula-backend/internal/storage"
)

// AuthProviders advertises configured sign-in methods without exposing provider credentials.
func (h *AuthHandler) AuthProviders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"providers": gin.H{"password": true, "github": h.Cfg.GitHub.Enabled}})
}

// GitHubStart creates a public, client-PKCE-bound authorization transaction.
func (h *AuthHandler) GitHubStart(c *gin.Context) { h.startGitHub(c, false) }

// GitHubLinkStart requires a current password in addition to the live owner JWT.
func (h *AuthHandler) GitHubLinkStart(c *gin.Context) { h.startGitHub(c, true) }

func (h *AuthHandler) startGitHub(c *gin.Context, link bool) {
	var input struct {
		Challenge string `json:"code_challenge"`
		Password  string `json:"current_password,omitempty"`
	}
	if !decodeAccountJSONBody(c, &input) {
		return
	}
	if !h.Cfg.GitHub.Enabled {
		writeGitHubError(c, githubauth.ErrDisabled)
		return
	}
	if !accounttoken.Valid(input.Challenge) || (!link && input.Password != "") {
		writeGitHubError(c, githubauth.ErrRequest)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	var intent storage.GitHubLinkIntent
	if link {
		intent.UserID = c.MustGet("userId").(string)
		user, err := storage.FindUserByUserId(ctx, h.DB, intent.UserID)
		if err != nil {
			writeGitHubError(c, err)
			return
		}
		intent.SessionVersion = user.SessionVersion
		// The version is captured before bcrypt; storage checks it again after reauthentication.
		session, ok := c.Get("sessionVersion")
		if !ok || session != intent.SessionVersion || len(input.Password) > 72 || !auth.CheckPasswordHash(input.Password, user.PasswordHash) {
			c.JSON(401, gin.H{"error": "Confirm your current password to link GitHub", "code": "reauthentication_required"})
			return
		}
	}
	result, err := h.GitHub.Start(ctx, input.Challenge, middleware.AccountClientIP(c), intent)
	if err != nil {
		writeGitHubError(c, err)
		return
	}
	c.JSON(200, result)
}

// GitHubCallback exchanges a frontend-captured code; credentials are never returned in redirects.
func (h *AuthHandler) GitHubCallback(c *gin.Context) {
	var input struct {
		Code     string `json:"code"`
		State    string `json:"state"`
		Verifier string `json:"code_verifier"`
	}
	if !decodeAccountJSONBody(c, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	user, linked, err := h.GitHub.Complete(ctx, input.Code, input.State, input.Verifier)
	if err != nil {
		writeGitHubError(c, err)
		return
	}
	if linked {
		c.JSON(200, gin.H{"message": "GitHub linked. Sign in again to continue."})
		return
	}
	token, err := auth.GenerateSessionJWT(user.UserId, h.Cfg.JWTSecret, h.Cfg.JWTExpiration, user.SessionVersion)
	if err != nil {
		writeGitHubError(c, err)
		return
	}
	c.JSON(200, models.LoginResponse{Message: "Logged in successfully", User: *user, Token: token})
}

func writeGitHubError(c *gin.Context, err error) {
	status, code, message := 503, "github_oauth_unavailable", "GitHub sign-in could not be completed. Start again."
	switch {
	case errors.Is(err, githubauth.ErrDisabled):
		code, message = "github_oauth_disabled", "GitHub sign-in is not configured"
	case errors.Is(err, githubauth.ErrBusy):
		code = "github_oauth_busy"
		c.Header("Retry-After", "5")
	case errors.Is(err, githubauth.ErrRequest):
		status, code = 400, "invalid_github_oauth_request"
	case errors.Is(err, storage.ErrGitHubState), errors.Is(err, githubauth.ErrProviderCode):
		status, code = 400, "invalid_github_oauth_state"
	case errors.Is(err, githubauth.ErrVerifiedEmail):
		status, code, message = 403, "github_verified_email_required", "GitHub must have a verified primary email address"
	case errors.Is(err, storage.ErrGitHubConflict), errors.Is(err, storage.ErrEmailExists):
		status, code, message = 409, "github_account_conflict", "Sign in with your existing method and explicitly link GitHub"
	case errors.Is(err, storage.ErrGitHubAccount), errors.Is(err, storage.ErrUserNotFound):
		status, code = 403, "github_account_unavailable"
	case errors.Is(err, storage.ErrGitHubQuota):
		status, code = 429, "github_oauth_rate_limited"
		c.Header("Retry-After", "600")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, code = 408, "request_timeout"
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"error": message, "code": code})
}
