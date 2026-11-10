package models

// Reasons a member is emailed about a discussion comment. A member can get one email per
// comment; it is sent when any reason that applies to them is switched on.
const (
	NotifyMention       = "mention"        // they were @mentioned in the comment
	NotifyMyFile        = "my_file"        // the discussion is about an entry of their PO file
	NotifyStarted       = "started"        // they started the discussion
	NotifyParticipated  = "participated"   // they commented in it before
	NotifyNewTopic      = "new_topic"      // a free discussion (topic) was started
	NotifyOtherTopic    = "other_topic"    // any other comment in a free discussion
	NotifyOtherGlossary = "other_glossary" // any other comment about a glossary term
	NotifyOtherEntry    = "other_entry"    // any other comment about an entry of someone else's file
)

// NotifyKinds lists the switches in the order the settings page shows them
var NotifyKinds = []string{NotifyMention, NotifyMyFile, NotifyStarted, NotifyParticipated,
	NotifyNewTopic, NotifyOtherTopic, NotifyOtherGlossary, NotifyOtherEntry}

// NotificationSettings are a member's email choices for one project (default: everything on)
type NotificationSettings struct {
	Muted bool            // no emails from this project at all
	Off   map[string]bool // kinds switched off
}

// Wants reports whether an email of this kind should be sent
func (n *NotificationSettings) Wants(kind string) bool {
	if n == nil {
		return true
	}
	return !n.Muted && !n.Off[kind]
}

// On reports whether a kind is switched on (for the settings form)
func (n *NotificationSettings) On(kind string) bool { return n == nil || !n.Off[kind] }
