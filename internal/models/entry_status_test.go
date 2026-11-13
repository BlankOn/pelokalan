package models

import "testing"

// An AI draft no person has saved has its own status, ahead of the fuzzy flag
func TestEntryStatusAI(t *testing.T) {
	cases := []struct {
		e    Entry
		want string
	}{
		{Entry{MsgID: "Open", MsgStr: []string{""}}, "untranslated"},
		{Entry{MsgID: "Open", MsgStr: []string{"Buka"}, Origin: OriginAI}, "ai"},
		{Entry{MsgID: "Open", MsgStr: []string{"Buka"}, Origin: OriginAI, Fuzzy: true}, "ai"}, // restored from an old snapshot
		{Entry{MsgID: "Open", MsgStr: []string{"Buka"}, Fuzzy: true}, "fuzzy"},
		{Entry{MsgID: "Open", MsgStr: []string{"Buka"}, Origin: OriginMemory, Fuzzy: true}, "fuzzy"},
		{Entry{MsgID: "Open", MsgStr: []string{"Buka"}}, "translated"},
	}
	for _, c := range cases {
		if got := c.e.Status(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.e, got, c.want)
		}
	}
}
