package avatar

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeBoundsAndStripsInput(t *testing.T) {
	var input bytes.Buffer
	require.NoError(t, png.Encode(&input, image.NewRGBA(image.Rect(0, 0, 400, 200))))
	photo, err := Normalize(append(input.Bytes(), []byte("private trailing metadata")...))
	require.NoError(t, err)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(photo))
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
	require.Equal(t, Size, cfg.Width)
	require.Equal(t, Size, cfg.Height)
	require.NotContains(t, string(photo), "private trailing metadata")
	for _, data := range [][]byte{nil, []byte("<svg onload='alert(1)'/>"), input.Bytes()[:15], make([]byte, MaxUploadBytes+1)} {
		_, err := Normalize(data)
		require.ErrorIs(t, err, ErrImage)
	}
	var wide bytes.Buffer
	require.NoError(t, png.Encode(&wide, image.NewRGBA(image.Rect(0, 0, 4097, 1))))
	_, err = Normalize(wide.Bytes())
	require.ErrorIs(t, err, ErrImage)
}

func TestGitHubImagesAreAllowlisted(t *testing.T) {
	for _, raw := range []string{"https://avatars.githubusercontent.com/u/123?v=4", "https://github.com/images/error/octocat_happy.gif"} {
		require.Equal(t, raw, GitHubURL(raw))
	}
	for _, raw := range []string{"https://evil.test/avatar", "//avatars.githubusercontent.com/u/1", "http://avatars.githubusercontent.com/u/1", "https://avatars.githubusercontent.com.evil.test/u/1", "https://secret@avatars.githubusercontent.com/u/1", "https://avatars.githubusercontent.com:8443/u/1", "https://github.com/redirect?url=evil", "data:image/svg+xml,test", "https://avatars.githubusercontent.com/u/1#x"} {
		require.Empty(t, GitHubURL(raw), raw)
	}
}
