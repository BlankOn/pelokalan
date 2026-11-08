package web

import (
	"path/filepath"
	"testing"

	"pelokalan/internal/config"
)

func TestTemplatesParse(t *testing.T) {
	_, err := NewHandler(filepath.Join("..", "..", "templates"), config.SiteConfig{Name: "Test"}, config.ServerConfig{}, nil, nil, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
}
