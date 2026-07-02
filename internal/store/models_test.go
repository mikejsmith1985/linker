package store

import "testing"

// TestWorkLocationValues guards the persisted work_location values, which are
// compared during scoring's work-location gate — changing them silently would
// break gating against existing rows.
func TestWorkLocationValues(t *testing.T) {
	cases := map[WorkLocation]string{
		WorkOnsite:  "onsite",
		WorkHybrid:  "hybrid",
		WorkRemote:  "remote",
		WorkUnknown: "unknown",
	}
	for loc, want := range cases {
		if string(loc) != want {
			t.Errorf("WorkLocation %v = %q, want %q", loc, string(loc), want)
		}
	}
}

// TestDocTypeValues guards the persisted doc_type values used as part of the
// (match_result_id, doc_type) uniqueness key.
func TestDocTypeValues(t *testing.T) {
	if string(TailoredResume) != "tailored_resume" {
		t.Errorf("TailoredResume = %q", TailoredResume)
	}
	if string(CoverLetter) != "cover_letter" {
		t.Errorf("CoverLetter = %q", CoverLetter)
	}
}
