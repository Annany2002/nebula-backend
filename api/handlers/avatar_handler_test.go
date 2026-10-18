package handlers_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Annany2002/nebula-backend/api/models"
	"github.com/Annany2002/nebula-backend/internal/avatar"
)

func uploadPhoto(t *testing.T, f *backendFixture, data []byte, authorization string, extra bool) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("avatar", "../../untrusted.svg")
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	if extra {
		require.NoError(t, form.WriteField("user_id", "someone-else"))
	}
	require.NoError(t, form.Close())
	req := httptest.NewRequest("POST", "/api/v1/account/user/me/avatar", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", authorization)
	out := httptest.NewRecorder()
	f.router.ServeHTTP(out, req)
	return out
}

func TestAvatarRoutesNormalizeImagesEnforceJWTAndRevokeOldAssets(t *testing.T) {
	f := newBackendFixture(t)
	var photo bytes.Buffer
	require.NoError(t, png.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 120, 200))))
	for _, header := range []string{"", "ApiKey not-an-owner-jwt"} {
		require.Equal(t, 401, uploadPhoto(t, f, photo.Bytes(), header, false).Code)
		for _, route := range []struct{ method, path string }{{"DELETE", "/api/v1/account/user/me/avatar"}, {"POST", "/api/v1/account/user/me/onboarding"}} {
			require.Equal(t, 401, f.request(route.method, route.path, header, "").Code)
		}
	}
	out := uploadPhoto(t, f, photo.Bytes(), "Bearer "+f.token, false)
	require.Equal(t, 200, out.Code, out.Body.String())
	var profile models.UserProfileResponse
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &profile))
	require.True(t, profile.OnboardingCompleted)
	require.Equal(t, "upload", profile.AvatarSource)
	asset := profile.AvatarURL
	out = f.request("GET", asset, "", "")
	require.Equal(t, 200, out.Code)
	require.Equal(t, "image/jpeg", out.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", out.Header().Get("X-Content-Type-Options"))
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out.Body.Bytes()))
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
	require.Equal(t, 256, cfg.Width)
	require.Equal(t, 256, cfg.Height)
	out = uploadPhoto(t, f, photo.Bytes(), "Bearer "+f.token, false)
	require.Equal(t, 200, out.Code)
	var repeated models.UserProfileResponse
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &repeated))
	require.Equal(t, asset, repeated.AvatarURL, "identical uploads are idempotent")
	replacement := image.NewRGBA(image.Rect(0, 0, 120, 200))
	draw.Draw(replacement, replacement.Bounds(), image.NewUniform(color.RGBA{R: 140, B: 220, A: 255}), image.Point{}, draw.Src)
	var changed bytes.Buffer
	require.NoError(t, png.Encode(&changed, replacement))
	out = uploadPhoto(t, f, changed.Bytes(), "Bearer "+f.token, false)
	require.Equal(t, 200, out.Code)
	require.Equal(t, 404, f.request("GET", asset, "", "").Code)
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &profile))
	asset = profile.AvatarURL
	require.Equal(t, 200, f.request("DELETE", "/api/v1/account/user/me/avatar", "Bearer "+f.token, "").Code)
	require.Equal(t, 404, f.request("GET", asset, "", "").Code)
	out = f.request("POST", "/api/v1/account/user/me/onboarding", "Bearer "+f.token, "")
	require.Equal(t, 200, out.Code)
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &profile))
	require.Empty(t, profile.AvatarURL)
	require.True(t, profile.OnboardingCompleted)
}

func TestInvalidAvatarsNeverChangeAnAccount(t *testing.T) {
	f := newBackendFixture(t)
	for _, data := range [][]byte{nil, []byte("<svg onload='bad'/>"), []byte("not-an-image")} {
		out := uploadPhoto(t, f, data, "Bearer "+f.token, false)
		require.Equal(t, 400, out.Code, out.Body.String())
	}
	require.Equal(t, 413, uploadPhoto(t, f, make([]byte, avatar.MaxUploadBytes+1), "Bearer "+f.token, false).Code)
	require.Equal(t, 400, uploadPhoto(t, f, []byte("x"), "Bearer "+f.token, true).Code)
	out := f.request("POST", "/api/v1/account/user/me/avatar", "Bearer "+f.token, strings.Repeat("x", 5000))
	require.Equal(t, 415, out.Code)
	out = f.request("GET", "/api/v1/account/user/me", "Bearer "+f.token, "")
	var profile models.UserProfileResponse
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &profile))
	require.False(t, profile.OnboardingCompleted)
	require.Empty(t, profile.AvatarURL)
}
