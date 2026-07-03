// Package store defines the job-matcher's domain types and the Postgres-backed
// persistence layer for resumes, preferences, searches, discovered openings,
// scored match results, generated documents, and selections.
package store

import "time"

// WorkLocation is the onsite/hybrid/remote nature of a job or a preference.
type WorkLocation string

const (
	WorkOnsite  WorkLocation = "onsite"
	WorkHybrid  WorkLocation = "hybrid"
	WorkRemote  WorkLocation = "remote"
	WorkUnknown WorkLocation = "unknown"
)

// SearchStatus is the lifecycle state of one discovery+scoring run.
type SearchStatus string

const (
	SearchRunning   SearchStatus = "running"
	SearchCompleted SearchStatus = "completed"
	SearchFailed    SearchStatus = "failed"
)

// DocType distinguishes the two kinds of generated document.
type DocType string

const (
	TailoredResume DocType = "tailored_resume"
	CoverLetter    DocType = "cover_letter"
)

// Resume is the user's single active resume plus its extracted fact set. RawText
// is the deterministically extracted text that the no-fabrication check runs
// against; StructuredProfile is the LLM-organized skills/experience summary.
type Resume struct {
	ID                int64
	OriginalFilename  string
	Format            string // pdf | docx | txt
	RawText           string
	StructuredProfile string
	IsActive          bool
	CreatedAt         time.Time
}

// Preferences are the scoring inputs. A single active row exists at a time.
type Preferences struct {
	ID                int64
	RequiredSalaryMin int // 0 = unset
	SalaryCurrency    string
	WorkLocationPref  WorkLocation
	// StrictWorkLocation hard-excludes roles that conflict with the work-location
	// preference (e.g. hybrid/onsite for a remote preference) instead of merely
	// penalizing them.
	StrictWorkLocation   bool
	Location             string // the user's base location/region, e.g. "United States"
	WillingToTravel      bool
	WillingToRelocate    bool
	BrowserAutomationAck bool
	EnabledSources       []string
	// TargetRoles are user-specified job titles to search for (e.g. aspirational
	// AI-first roles), searched alongside the roles derived from the resume.
	TargetRoles []string
	// NewRolesOnly excludes postings already seen in a previous search, so a
	// search surfaces only roles the user hasn't encountered yet.
	NewRolesOnly bool
	UpdatedAt    time.Time
}

// Search is one on-demand discovery+scoring run.
type Search struct {
	ID                  int64
	ResumeID            int64
	PreferencesSnapshot Preferences
	Status              SearchStatus
	SourceHealth        map[string]string // source name -> succeeded|failed|no_results
	StartedAt           time.Time
	FinishedAt          *time.Time
}

// JobOpening is a discovered posting, de-duplicated by CanonicalKey.
type JobOpening struct {
	ID               int64
	CanonicalKey     string
	Title            string
	Employer         string
	Location         string
	WorkLocationType WorkLocation
	SalaryMin        int // 0 = unstated
	SalaryMax        int // 0 = unstated
	Description      string
	SourceNames      []string
	OriginalURL      string
	EmployerWebsite  string // the employer's own site, for a direct-apply link
	ReviewStatus     string // new | interested | passed
	ReviewReason     string // optional note on why a job was passed
	DiscoveredAt     time.Time
}

// Review states for a job opening, persisted so a mark survives re-runs.
const (
	ReviewNew        = "new"
	ReviewInterested = "interested"
	ReviewPassed     = "passed"
)

// MatchResult pairs one opening with a search's resume+preferences: the score
// and its explanation. Results below the threshold are stored but never shown.
type MatchResult struct {
	ID               int64
	SearchID         int64
	JobOpeningID     int64
	Score            int
	ScoreExplanation string
	GatePenalties    map[string]int
	IsQualifying     bool
	Rank             int
}

// MatchWithOpening joins a match result to its opening for display.
type MatchWithOpening struct {
	MatchResult
	Opening JobOpening
}

// GeneratedDocument is a tailored resume or cover letter for one match result.
type GeneratedDocument struct {
	ID               int64
	MatchResultID    int64
	Type             DocType
	ContentMarkdown  string
	FabricationFlags []string
	WasEditedByUser  bool
	GeneratedAt      time.Time
}

// SearchSummary is a search plus its qualifying-match count, for the recent
// searches feedback list.
type SearchSummary struct {
	Search
	QualifyingCount int
}

// ChatMessage is one turn in the in-app assistant conversation.
type ChatMessage struct {
	ID        int64
	Role      string // user | assistant
	Content   string
	CreatedAt time.Time
}

// Selection records the user's decision to pursue an opening and that its
// posting was opened for manual submission. The system never auto-submits.
type Selection struct {
	ID               int64
	MatchResultID    int64
	WasPostingOpened bool
	SelectedAt       time.Time
}
