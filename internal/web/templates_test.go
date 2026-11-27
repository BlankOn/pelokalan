package web

import (
	"testing"

	"pelokalan/internal/platform/config"
)

func TestTemplatesParse(t *testing.T) {
	_, err := NewHandler("templates", config.SiteConfig{Name: "Test"}, config.ServerConfig{}, nil, nil, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
}
