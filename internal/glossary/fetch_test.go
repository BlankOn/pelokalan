package glossary

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchBlocksLocalAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("x")) }))
	defer srv.Close()
	_, err := NewFetcher(1<<20, false, "test").Fetch(context.Background(), srv.URL, "", "")
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("err = %v, want blocked", err)
	}
}

func TestFetchConditionalAndLimits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte(poA))
	}))
	defer srv.Close()
	f := NewFetcher(1<<20, true, "test")

	res, err := f.Fetch(context.Background(), srv.URL+"/languages/id/gnome-51/ui.po", "", "")
	if err != nil || res.ETag != `"v1"` || res.Filename != "ui.po" || string(res.Data) != poA {
		t.Fatalf("fetch: %v %+v", err, res)
	}
	if _, err := f.Fetch(context.Background(), srv.URL, `"v1"`, ""); !errors.Is(err, ErrNotModified) {
		t.Fatalf("conditional: %v", err)
	}
	if _, err := NewFetcher(10, true, "test").Fetch(context.Background(), srv.URL, "", ""); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("limit: %v", err)
	}
	for _, bad := range []string{"ftp://x/y", "file:///etc/passwd", "http://user:pw@host/x", "not a url"} {
		if _, err := ValidateURL(bad); err == nil {
			t.Errorf("ValidateURL(%q) accepted", bad)
		}
	}
}
