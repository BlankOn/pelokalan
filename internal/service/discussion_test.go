package service

import (
	"strings"
	"testing"
)

func TestMentions(t *testing.T) {
	got := Mentions("Hi @ani and @Budi.S, see admin@blankon.id and @ani again. @x @ok_user.")
	want := "ani budi.s ok_user"
	if strings.Join(got, " ") != want {
		t.Errorf("mentions = %v, want %s", got, want)
	}
}
