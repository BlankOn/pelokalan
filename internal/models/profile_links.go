package models

import "strings"

// LinkKinds are the public profile links, in display order
var LinkKinds = []string{"website", "github", "x", "linkedin", "mastodon"}

// ProfileLink is one link on a public profile
type ProfileLink struct {
	Kind  string
	URL   string
	Label string // shown text: the address without its scheme ("github.com/name")
}

// PublicLinks are the user's links in display order
func (u *User) PublicLinks() []ProfileLink {
	var out []ProfileLink
	for _, k := range LinkKinds {
		if v := u.Links[k]; v != "" {
			label := strings.TrimPrefix(strings.TrimPrefix(v, "https://"), "http://")
			label = strings.TrimPrefix(strings.TrimSuffix(label, "/"), "www.")
			out = append(out, ProfileLink{Kind: k, URL: v, Label: label})
		}
	}
	return out
}

// Link returns one link (for the profile form)
func (u *User) Link(kind string) string { return u.Links[kind] }
