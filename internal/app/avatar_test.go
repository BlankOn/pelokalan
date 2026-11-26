package app_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

// postAvatar uploads a profile picture like the crop dialog does
func (u *user) postAvatar(data []byte) *response {
	u.t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, _ := w.CreateFormFile("avatar", "avatar.jpg")
	_, _ = fw.Write(data)
	_ = w.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/profile/avatar", &b)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return u.do(req)
}

// Profile pictures: any picture becomes a 256×256 JPEG in the blob store, served by its content hash and shown
// on the public page and the members page; non-pictures are refused; removing it brings the initial back
func TestAvatars(t *testing.T) {
	skipWithoutDB(t)
	nadia := signUp(t, "nadia")
	slug := nadia.newProject("Foto")

	src := image.NewRGBA(image.Rect(0, 0, 900, 600)) // not square: the server cuts the middle
	for y := 0; y < 600; y++ {
		for x := 0; x < 900; x++ {
			src.Set(x, y, color.RGBA{uint8(x / 4), 90, uint8(y / 3), 255})
		}
	}
	var pic bytes.Buffer
	if err := png.Encode(&pic, src); err != nil {
		t.Fatal(err)
	}
	r := nadia.postAvatar(pic.Bytes()).status(http.StatusOK)
	var res struct{ URL string }
	if err := json.Unmarshal([]byte(r.Body), &res); err != nil || !strings.HasPrefix(res.URL, "/avatars/") {
		t.Fatalf("upload answer %q", r.Body)
	}
	img := visitor(t).get(res.URL).status(http.StatusOK)
	if img.Header.Get("Content-Type") != "image/jpeg" || !strings.Contains(img.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("avatar headers %v", img.Header)
	}
	cfg, err := jpeg.DecodeConfig(strings.NewReader(img.Body))
	if err != nil || cfg.Width != 256 || cfg.Height != 256 {
		t.Fatalf("stored avatar %dx%d (%v)", cfg.Width, cfg.Height, err)
	}
	visitor(t).get("/users/" + nadia.Username).status(http.StatusOK).contains(res.URL)
	visitor(t).get("/projects/" + slug + "/members").status(http.StatusOK).contains(res.URL)

	// not a picture; other blobs are never served at the avatar address
	nadia.postAvatar([]byte("hello")).status(http.StatusBadRequest).contains("bukan gambar")
	visitor(t).get("/avatars/" + strings.Repeat("a", 64) + ".jpg").status(http.StatusNotFound)
	visitor(t).get("/avatars/../etc.jpg").status(http.StatusNotFound)

	nadia.post("/profile/avatar/delete", nil).status(http.StatusOK)
	visitor(t).get(res.URL).status(http.StatusNotFound)
	visitor(t).get("/users/" + nadia.Username).status(http.StatusOK).lacks(res.URL)
	// visitors cannot upload
	if visitor(t).postAvatar(pic.Bytes()).Status == http.StatusOK {
		t.Fatal("a visitor uploaded a profile picture")
	}
}
