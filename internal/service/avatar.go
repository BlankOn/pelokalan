package service

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"

	// decoders for uploaded pictures
	_ "image/gif"
	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"pelokalan/internal/models"
)

// Profile pictures: whatever people upload (already cropped by the browser, or not) is cut to a centred square,
// scaled to AvatarSize and stored as a JPEG in the blob store; users.avatar holds the blob key.
const (
	AvatarSize      = 256
	MaxAvatarUpload = 10 << 20 // bytes
	maxAvatarPixels = 40_000_000
	avatarQuality   = 85
)

// ErrAvatarInvalid is returned for files that are not a usable picture
var ErrAvatarInvalid = userErr("err.avatar_invalid")

// MakeAvatar turns an uploaded picture (JPEG, PNG, GIF or WebP) into the stored square JPEG
func MakeAvatar(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxAvatarUpload {
		return nil, ErrAvatarInvalid
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 16 || cfg.Height < 16 || cfg.Width*cfg.Height > maxAvatarPixels {
		return nil, ErrAvatarInvalid // also refuses tiny files that claim huge sizes (decompression bombs)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrAvatarInvalid
	}
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	crop := image.Rect(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2, 0, 0)
	crop.Max = crop.Min.Add(image.Pt(side, side))
	dst := image.NewRGBA(image.Rect(0, 0, AvatarSize, AvatarSize))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src) // transparent parts become white
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: avatarQuality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// SetAvatar stores a new profile picture for the user (the old one is left to the blob GC)
func (s *AuthService) SetAvatar(ctx context.Context, user *models.User, data []byte) error {
	if s.blobs == nil {
		return ErrAvatarInvalid
	}
	pic, err := MakeAvatar(data)
	if err != nil {
		return err
	}
	key, err := s.blobs.Put(ctx, pic)
	if err != nil {
		return err
	}
	if err := s.users.SetAvatar(ctx, user.ID, key); err != nil {
		return err
	}
	user.Avatar = key
	return nil
}

// RemoveAvatar takes the profile picture away (initials show again)
func (s *AuthService) RemoveAvatar(ctx context.Context, user *models.User) error {
	if err := s.users.SetAvatar(ctx, user.ID, ""); err != nil {
		return err
	}
	user.Avatar = ""
	return nil
}

// AvatarData returns a profile picture by its key, only while someone uses it (so the address never serves
// other blobs, such as comment attachments)
func (s *AuthService) AvatarData(ctx context.Context, key string) ([]byte, error) {
	if s.blobs == nil || !s.users.AvatarInUse(ctx, key) {
		return nil, ErrNotFound
	}
	return s.blobs.Get(ctx, key)
}
