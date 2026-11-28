package app_test

import (
	"net/http"
	"strings"
	"testing"
)

// The top bar's project switcher: the button names the current project; its menu (loaded on open) lists the
// person's projects with their role, marks the current one and links to settings for owners and coordinators
func TestProjectSwitch(t *testing.T) {
	skipWithoutDB(t)
	owner := signUp(t, "pswowner")
	alpha := owner.newProject("Alpha switch")
	beta := owner.newProject("Beta switch")
	tina := signUp(t, "pswtina")
	addMember(t, owner, beta, tina, "translator")

	owner.get("/projects/"+alpha+"/files").status(http.StatusOK).
		contains(`class="projswitch" data-switch-url="/nav/projects?current=`+alpha+`"`, `<span class="psw-label">Alpha switch</span>`)

	menu := owner.get("/nav/projects?current=" + alpha).status(http.StatusOK).Body
	for _, want := range []string{"Alpha switch", "Beta switch", "Pemilik", `class="psw-item active" href="/projects/` + alpha + `/files"`,
		`href="/projects/` + alpha + `/settings"`, `href="/projects?new=1"`} {
		if !strings.Contains(menu, want) {
			t.Fatalf("menu lacks %q:\n%s", want, menu)
		}
	}
	if strings.Index(menu, "Alpha switch") > strings.Index(menu, "Beta switch") {
		t.Fatal("projects are not sorted by name")
	}

	// a translator: only their projects, no settings link
	menu = tina.get("/nav/projects?current=" + beta).status(http.StatusOK).Body
	if !strings.Contains(menu, "Beta switch") || strings.Contains(menu, "Alpha switch") || !strings.Contains(menu, "Penerjemah") ||
		strings.Contains(menu, "/settings") {
		t.Fatalf("translator menu:\n%s", menu)
	}

	// visitors see the projects, without roles or "new project"
	menu = visitor(t).get("/nav/projects").status(http.StatusOK).Body
	if !strings.Contains(menu, "Alpha switch") || strings.Contains(menu, "psw-role") || strings.Contains(menu, "new=1") {
		t.Fatalf("visitor menu:\n%s", menu)
	}
}
