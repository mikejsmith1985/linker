package store

import "testing"

// TestEventTypeValues guards the string values of event types, which double as
// the persisted `event_type` column and the dedup key — changing them silently
// would orphan existing rows.
func TestEventTypeValues(t *testing.T) {
	cases := map[EventType]string{
		EventCommit:  "commit",
		EventRelease: "release",
		EventReadme:  "readme",
	}
	for typ, want := range cases {
		if string(typ) != want {
			t.Errorf("EventType %v = %q, want %q", typ, string(typ), want)
		}
	}
}

// TestPostStatusValues guards the persisted lifecycle values.
func TestPostStatusValues(t *testing.T) {
	cases := map[PostStatus]string{
		StatusDraft:     "draft",
		StatusQueued:    "queued",
		StatusPublished: "published",
		StatusRejected:  "rejected",
	}
	for status, want := range cases {
		if string(status) != want {
			t.Errorf("PostStatus %v = %q, want %q", status, string(status), want)
		}
	}
}
