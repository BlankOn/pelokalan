package service

import (
	"testing"

	"pelokalan/internal/models"
	"pelokalan/internal/po"
)

func TestCheckProjectLanguage(t *testing.T) {
	p := &models.Project{Language: "id"}
	for _, ok := range []string{"id", "ID", "", " id "} {
		if err := checkProjectLanguage(p, ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	err := checkProjectLanguage(p, "jv")
	ue, isUser := AsUserError(err)
	if !isUser || ue.Key != "err.language_mismatch" || ue.Args[0] != "jv" || ue.Args[1] != "id" {
		t.Fatalf("jv in an id project: %v", err)
	}
	if checkProjectLanguage(&models.Project{}, "jv") != nil {
		t.Error("a project without a language must accept anything")
	}
	if !po.SameLanguage("pt-BR", "pt_br") || po.SameLanguage("pt", "pt_BR") {
		t.Error("SameLanguage")
	}
}
