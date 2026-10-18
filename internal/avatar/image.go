// Package avatar normalizes untrusted profile images before persistence.
package avatar

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png" // Register PNG decoding.
	"net/url"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // Register WebP decoding.
)

const MaxUploadBytes = 2 * 1024 * 1024
const Size = 256

var ErrImage = errors.New("use a JPEG, PNG or WebP image up to 2 MiB and 4096 pixels per side")

// Normalize bounds allocation before decoding, center-crops and strips metadata/active content.
func Normalize(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxUploadBytes {
		return nil, ErrImage
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "webp") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 8*1024*1024 {
		return nil, ErrImage
	}
	src, decodedFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil || decodedFormat != format || src.Bounds().Dx() != cfg.Width || src.Bounds().Dy() != cfg.Height {
		return nil, ErrImage
	}
	crop := src.Bounds()
	side := min(crop.Dx(), crop.Dy())
	crop.Min.X += (crop.Dx() - side) / 2
	crop.Min.Y += (crop.Dy() - side) / 2
	crop.Max = crop.Min.Add(image.Pt(side, side))
	dst := image.NewRGBA(image.Rect(0, 0, Size, Size))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.BiLinear.Scale(dst, dst.Bounds(), src, crop, draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// GitHubURL accepts only GitHub-owned HTTPS images. The backend never fetches this URL.
func GitHubURL(raw string) string {
	if raw == "" || len(raw) > 2048 {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Port() != "" || u.RawPath != "" {
		return ""
	}
	if u.Host != "avatars.githubusercontent.com" && u.Host != "github.com" {
		return ""
	}
	if u.Host == "github.com" && u.Path != "/images/error/octocat_happy.gif" {
		return ""
	}
	if u.Host == "avatars.githubusercontent.com" && (len(u.Path) < 2 || u.Path[0] != '/') {
		return ""
	}
	return u.String()
}
