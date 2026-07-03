package store

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed schema.sql
var schemaSQL string

// ErrNotFound is returned by single-row getters when nothing matches.
var ErrNotFound = errors.New("not found")

// DB is the subset of the pgx pool API the store needs. Both *pgxpool.Pool and
// pgxmock's pool interface satisfy it, so the store is unit-testable without a
// real database.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Store is the persistence interface consumed by the orchestrator and web
// layers. Consumers accept the interface so they can be tested with fakes.
type Store interface {
	RunMigrations(ctx context.Context) error

	SaveResume(ctx context.Context, r Resume) (int64, error)
	GetActiveResume(ctx context.Context) (Resume, error)

	SavePreferences(ctx context.Context, p Preferences) (int64, error)
	GetPreferences(ctx context.Context) (Preferences, error)

	CreateSearch(ctx context.Context, resumeID int64, snapshot Preferences) (int64, error)
	FinishSearch(ctx context.Context, id int64, status SearchStatus, health map[string]string) error
	GetSearch(ctx context.Context, id int64) (Search, error)
	LatestCompletedSearchID(ctx context.Context) (int64, error)
	ListRecentSearches(ctx context.Context, limit int) ([]SearchSummary, error)
	FailRunningSearches(ctx context.Context) error

	UpsertOpening(ctx context.Context, o JobOpening) (int64, error)
	FindScoredOpening(ctx context.Context, canonicalKey string) (MatchResult, error)
	SetOpeningReviewStatus(ctx context.Context, openingID int64, status, reason string) error

	CreateMatchResult(ctx context.Context, m MatchResult) (int64, error)
	ListQualifying(ctx context.Context, searchID int64) ([]MatchWithOpening, error)
	GetMatchWithOpening(ctx context.Context, matchID int64) (MatchWithOpening, error)

	SaveDocument(ctx context.Context, d GeneratedDocument) (int64, error)
	GetDocument(ctx context.Context, matchID int64, docType DocType) (GeneratedDocument, error)
	UpdateDocumentContent(ctx context.Context, id int64, content string) error

	UpsertSelection(ctx context.Context, matchID int64, opened bool) error

	AppendChatMessage(ctx context.Context, role, content string) error
	ListChatMessages(ctx context.Context, limit int) ([]ChatMessage, error)
}

// PG is the Postgres-backed Store implementation.
type PG struct{ db DB }

// New returns a Store backed by the given DB handle.
func New(db DB) *PG { return &PG{db: db} }

// RunMigrations applies the embedded schema. It is idempotent.
func (s *PG) RunMigrations(ctx context.Context) error {
	if _, err := s.db.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// ── Resume ──

// SaveResume deactivates any existing active resume and inserts the new one as
// active, so exactly one resume is active at a time (prior searches keep
// pointing at the resume row that produced them).
func (s *PG) SaveResume(ctx context.Context, r Resume) (int64, error) {
	if _, err := s.db.Exec(ctx, `UPDATE resumes SET is_active = FALSE WHERE is_active = TRUE`); err != nil {
		return 0, fmt.Errorf("deactivate resumes: %w", err)
	}
	var id int64
	err := s.db.QueryRow(ctx,
		`INSERT INTO resumes (original_filename, format, raw_text, structured_profile, is_active)
		 VALUES ($1, $2, $3, $4, TRUE) RETURNING id`,
		r.OriginalFilename, r.Format, r.RawText, r.StructuredProfile,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert resume: %w", err)
	}
	return id, nil
}

// GetActiveResume returns the active resume, or ErrNotFound when none exists.
func (s *PG) GetActiveResume(ctx context.Context) (Resume, error) {
	var r Resume
	err := s.db.QueryRow(ctx,
		`SELECT id, original_filename, format, raw_text, structured_profile, is_active, created_at
		   FROM resumes WHERE is_active = TRUE ORDER BY id DESC LIMIT 1`,
	).Scan(&r.ID, &r.OriginalFilename, &r.Format, &r.RawText, &r.StructuredProfile, &r.IsActive, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Resume{}, ErrNotFound
	}
	if err != nil {
		return Resume{}, fmt.Errorf("get active resume: %w", err)
	}
	return r, nil
}

// ── Preferences ──

// SavePreferences keeps a single preferences row: it clears any existing rows
// and inserts the supplied one.
func (s *PG) SavePreferences(ctx context.Context, p Preferences) (int64, error) {
	if _, err := s.db.Exec(ctx, `DELETE FROM preferences`); err != nil {
		return 0, fmt.Errorf("clear preferences: %w", err)
	}
	sources, err := json.Marshal(nonNilStrings(p.EnabledSources))
	if err != nil {
		return 0, fmt.Errorf("marshal enabled_sources: %w", err)
	}
	roles, err := json.Marshal(nonNilStrings(p.TargetRoles))
	if err != nil {
		return 0, fmt.Errorf("marshal target_roles: %w", err)
	}
	var id int64
	err = s.db.QueryRow(ctx,
		`INSERT INTO preferences
		   (required_salary_min, salary_currency, work_location_pref, strict_work_location, location,
		    willing_to_travel, willing_to_relocate, browser_automation_ack, enabled_sources, target_roles,
		    new_roles_only, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now()) RETURNING id`,
		p.RequiredSalaryMin, defaultString(p.SalaryCurrency, "USD"), string(defaultWorkLocation(p.WorkLocationPref)),
		p.StrictWorkLocation, defaultString(p.Location, "United States"),
		p.WillingToTravel, p.WillingToRelocate, p.BrowserAutomationAck, string(sources), string(roles),
		p.NewRolesOnly,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert preferences: %w", err)
	}
	return id, nil
}

// GetPreferences returns the saved preferences, or sensible defaults when none
// have been saved yet.
func (s *PG) GetPreferences(ctx context.Context) (Preferences, error) {
	var p Preferences
	var loc string
	var sources, roles []byte
	err := s.db.QueryRow(ctx,
		`SELECT id, required_salary_min, salary_currency, work_location_pref, strict_work_location, location,
		        willing_to_travel, willing_to_relocate, browser_automation_ack, enabled_sources, target_roles,
		        new_roles_only, updated_at
		   FROM preferences ORDER BY id DESC LIMIT 1`,
	).Scan(&p.ID, &p.RequiredSalaryMin, &p.SalaryCurrency, &loc, &p.StrictWorkLocation, &p.Location, &p.WillingToTravel,
		&p.WillingToRelocate, &p.BrowserAutomationAck, &sources, &roles, &p.NewRolesOnly, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Preferences{SalaryCurrency: "USD", WorkLocationPref: WorkRemote, StrictWorkLocation: true, Location: "United States"}, nil
	}
	if err != nil {
		return Preferences{}, fmt.Errorf("get preferences: %w", err)
	}
	p.WorkLocationPref = WorkLocation(loc)
	if err := json.Unmarshal(sources, &p.EnabledSources); err != nil {
		return Preferences{}, fmt.Errorf("unmarshal enabled_sources: %w", err)
	}
	if err := json.Unmarshal(roles, &p.TargetRoles); err != nil {
		return Preferences{}, fmt.Errorf("unmarshal target_roles: %w", err)
	}
	return p, nil
}

// ── Search ──

// CreateSearch opens a new running search with a snapshot of the preferences.
func (s *PG) CreateSearch(ctx context.Context, resumeID int64, snapshot Preferences) (int64, error) {
	snap, err := json.Marshal(snapshot)
	if err != nil {
		return 0, fmt.Errorf("marshal snapshot: %w", err)
	}
	var id int64
	err = s.db.QueryRow(ctx,
		`INSERT INTO searches (resume_id, preferences_snapshot, status)
		 VALUES ($1, $2, 'running') RETURNING id`,
		resumeID, string(snap),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create search: %w", err)
	}
	return id, nil
}

// FinishSearch records the terminal status and per-source health of a search.
func (s *PG) FinishSearch(ctx context.Context, id int64, status SearchStatus, health map[string]string) error {
	h, err := json.Marshal(nonNilHealth(health))
	if err != nil {
		return fmt.Errorf("marshal health: %w", err)
	}
	_, err = s.db.Exec(ctx,
		`UPDATE searches SET status = $2, source_health = $3, finished_at = now() WHERE id = $1`,
		id, string(status), string(h))
	if err != nil {
		return fmt.Errorf("finish search: %w", err)
	}
	return nil
}

// LatestCompletedSearchID returns the id of the most recent completed search, or
// ErrNotFound when none exists yet.
func (s *PG) LatestCompletedSearchID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx,
		`SELECT id FROM searches WHERE status = 'completed' ORDER BY id DESC LIMIT 1`,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("latest search: %w", err)
	}
	return id, nil
}

// ListRecentSearches returns the most recent searches with their qualifying-match
// counts, newest first, for the search-activity feedback list.
func (s *PG) ListRecentSearches(ctx context.Context, limit int) ([]SearchSummary, error) {
	rows, err := s.db.Query(ctx,
		`SELECT s.id, s.resume_id, s.status, s.started_at, s.finished_at,
		        COALESCE(SUM(CASE WHEN m.is_qualifying THEN 1 ELSE 0 END), 0) AS qualifying
		   FROM searches s
		   LEFT JOIN match_results m ON m.search_id = s.id
		  GROUP BY s.id
		  ORDER BY s.id DESC
		  LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent searches: %w", err)
	}
	defer rows.Close()
	var out []SearchSummary
	for rows.Next() {
		var sum SearchSummary
		if err := rows.Scan(&sum.ID, &sum.ResumeID, &sum.Status, &sum.StartedAt, &sum.FinishedAt, &sum.QualifyingCount); err != nil {
			return nil, fmt.Errorf("scan recent search: %w", err)
		}
		out = append(out, sum)
	}
	return out, rows.Err()
}

// FailRunningSearches marks any search left in the running state as failed. It is
// called at startup so a search interrupted by a restart doesn't hang forever.
func (s *PG) FailRunningSearches(ctx context.Context) error {
	_, err := s.db.Exec(ctx,
		`UPDATE searches SET status = 'failed', finished_at = now() WHERE status = 'running'`)
	if err != nil {
		return fmt.Errorf("fail running searches: %w", err)
	}
	return nil
}

// GetSearch loads a search by id.
func (s *PG) GetSearch(ctx context.Context, id int64) (Search, error) {
	var sr Search
	var snap, health []byte
	err := s.db.QueryRow(ctx,
		`SELECT id, resume_id, preferences_snapshot, status, source_health, started_at, finished_at
		   FROM searches WHERE id = $1`, id,
	).Scan(&sr.ID, &sr.ResumeID, &snap, &sr.Status, &health, &sr.StartedAt, &sr.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Search{}, ErrNotFound
	}
	if err != nil {
		return Search{}, fmt.Errorf("get search: %w", err)
	}
	_ = json.Unmarshal(snap, &sr.PreferencesSnapshot)
	_ = json.Unmarshal(health, &sr.SourceHealth)
	return sr, nil
}

// ── JobOpening ──

// UpsertOpening inserts or updates an opening keyed by canonical_key, returning
// its id. Source names supplied by the caller replace the stored set (the
// registry has already merged duplicates before persisting).
func (s *PG) UpsertOpening(ctx context.Context, o JobOpening) (int64, error) {
	names, err := json.Marshal(nonNilStrings(o.SourceNames))
	if err != nil {
		return 0, fmt.Errorf("marshal source_names: %w", err)
	}
	var id int64
	err = s.db.QueryRow(ctx,
		`INSERT INTO job_openings
		   (canonical_key, title, employer, location, work_location_type, salary_min, salary_max,
		    description, source_names, original_url, employer_website)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		 ON CONFLICT (canonical_key) DO UPDATE SET
		   title = EXCLUDED.title, employer = EXCLUDED.employer, location = EXCLUDED.location,
		   work_location_type = EXCLUDED.work_location_type, salary_min = EXCLUDED.salary_min,
		   salary_max = EXCLUDED.salary_max, description = EXCLUDED.description,
		   source_names = EXCLUDED.source_names, original_url = EXCLUDED.original_url,
		   employer_website = EXCLUDED.employer_website
		 RETURNING id`,
		o.CanonicalKey, o.Title, o.Employer, o.Location, string(defaultWorkLocation(o.WorkLocationType)),
		o.SalaryMin, o.SalaryMax, o.Description, string(names), o.OriginalURL, o.EmployerWebsite,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert opening: %w", err)
	}
	return id, nil
}

// SetOpeningReviewStatus persists a Pass/Interested/new mark on the opening, so
// it survives future searches (the opening row is reused across searches).
func (s *PG) SetOpeningReviewStatus(ctx context.Context, openingID int64, status, reason string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE job_openings SET review_status = $2, review_reason = $3 WHERE id = $1`,
		openingID, status, reason)
	if err != nil {
		return fmt.Errorf("set review status: %w", err)
	}
	return nil
}

// FindScoredOpening returns the most recent match result for the opening with
// the given canonical key, so re-runs can reuse a prior score (FR-025).
func (s *PG) FindScoredOpening(ctx context.Context, canonicalKey string) (MatchResult, error) {
	var m MatchResult
	var penalties []byte
	err := s.db.QueryRow(ctx,
		`SELECT m.id, m.search_id, m.job_opening_id, m.score, m.score_explanation,
		        m.gate_penalties, m.is_qualifying, m.rank
		   FROM match_results m JOIN job_openings o ON o.id = m.job_opening_id
		  WHERE o.canonical_key = $1 ORDER BY m.id DESC LIMIT 1`, canonicalKey,
	).Scan(&m.ID, &m.SearchID, &m.JobOpeningID, &m.Score, &m.ScoreExplanation,
		&penalties, &m.IsQualifying, &m.Rank)
	if errors.Is(err, pgx.ErrNoRows) {
		return MatchResult{}, ErrNotFound
	}
	if err != nil {
		return MatchResult{}, fmt.Errorf("find scored opening: %w", err)
	}
	_ = json.Unmarshal(penalties, &m.GatePenalties)
	return m, nil
}

// ── MatchResult ──

// CreateMatchResult inserts a scored match.
func (s *PG) CreateMatchResult(ctx context.Context, m MatchResult) (int64, error) {
	penalties, err := json.Marshal(nonNilPenalties(m.GatePenalties))
	if err != nil {
		return 0, fmt.Errorf("marshal gate_penalties: %w", err)
	}
	var id int64
	err = s.db.QueryRow(ctx,
		`INSERT INTO match_results
		   (search_id, job_opening_id, score, score_explanation, gate_penalties, is_qualifying, rank)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		m.SearchID, m.JobOpeningID, m.Score, m.ScoreExplanation, string(penalties), m.IsQualifying, m.Rank,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create match result: %w", err)
	}
	return id, nil
}

// ListQualifying returns the qualifying (score>=70) matches of a search joined
// to their openings, ordered by rank ascending (best first).
func (s *PG) ListQualifying(ctx context.Context, searchID int64) ([]MatchWithOpening, error) {
	rows, err := s.db.Query(ctx,
		`SELECT m.id, m.search_id, m.job_opening_id, m.score, m.score_explanation,
		        m.gate_penalties, m.is_qualifying, m.rank,
		        o.id, o.canonical_key, o.title, o.employer, o.location, o.work_location_type,
		        o.salary_min, o.salary_max, o.description, o.source_names, o.original_url, o.employer_website, o.review_status, o.review_reason, o.discovered_at
		   FROM match_results m JOIN job_openings o ON o.id = m.job_opening_id
		  WHERE m.search_id = $1 AND m.is_qualifying = TRUE
		  ORDER BY m.rank ASC`, searchID)
	if err != nil {
		return nil, fmt.Errorf("list qualifying: %w", err)
	}
	defer rows.Close()

	var out []MatchWithOpening
	for rows.Next() {
		mw, err := scanMatchWithOpening(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, mw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate qualifying: %w", err)
	}
	return out, nil
}

// GetMatchWithOpening loads a single match joined to its opening.
func (s *PG) GetMatchWithOpening(ctx context.Context, matchID int64) (MatchWithOpening, error) {
	row := s.db.QueryRow(ctx,
		`SELECT m.id, m.search_id, m.job_opening_id, m.score, m.score_explanation,
		        m.gate_penalties, m.is_qualifying, m.rank,
		        o.id, o.canonical_key, o.title, o.employer, o.location, o.work_location_type,
		        o.salary_min, o.salary_max, o.description, o.source_names, o.original_url, o.employer_website, o.review_status, o.review_reason, o.discovered_at
		   FROM match_results m JOIN job_openings o ON o.id = m.job_opening_id
		  WHERE m.id = $1`, matchID)
	mw, err := scanMatchWithOpening(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return MatchWithOpening{}, ErrNotFound
	}
	if err != nil {
		return MatchWithOpening{}, err
	}
	return mw, nil
}

// scanDest abstracts *pgx.Row and pgx.Rows so one scan helper serves both.
type scanDest interface {
	Scan(dest ...any) error
}

func scanMatchWithOpening(row scanDest) (MatchWithOpening, error) {
	var mw MatchWithOpening
	var penalties, names []byte
	var loc string
	err := row.Scan(
		&mw.ID, &mw.SearchID, &mw.JobOpeningID, &mw.Score, &mw.ScoreExplanation,
		&penalties, &mw.IsQualifying, &mw.Rank,
		&mw.Opening.ID, &mw.Opening.CanonicalKey, &mw.Opening.Title, &mw.Opening.Employer,
		&mw.Opening.Location, &loc, &mw.Opening.SalaryMin, &mw.Opening.SalaryMax,
		&mw.Opening.Description, &names, &mw.Opening.OriginalURL, &mw.Opening.EmployerWebsite,
		&mw.Opening.ReviewStatus, &mw.Opening.ReviewReason, &mw.Opening.DiscoveredAt,
	)
	if err != nil {
		return MatchWithOpening{}, err
	}
	mw.Opening.WorkLocationType = WorkLocation(loc)
	_ = json.Unmarshal(penalties, &mw.GatePenalties)
	_ = json.Unmarshal(names, &mw.Opening.SourceNames)
	return mw, nil
}

// ── GeneratedDocument ──

// SaveDocument upserts a generated document keyed by (match_result_id, doc_type).
func (s *PG) SaveDocument(ctx context.Context, d GeneratedDocument) (int64, error) {
	flags, err := json.Marshal(nonNilStrings(d.FabricationFlags))
	if err != nil {
		return 0, fmt.Errorf("marshal fabrication_flags: %w", err)
	}
	var id int64
	err = s.db.QueryRow(ctx,
		`INSERT INTO generated_documents (match_result_id, doc_type, content_markdown, fabrication_flags, was_edited)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (match_result_id, doc_type) DO UPDATE SET
		   content_markdown = EXCLUDED.content_markdown,
		   fabrication_flags = EXCLUDED.fabrication_flags,
		   was_edited = EXCLUDED.was_edited,
		   generated_at = now()
		 RETURNING id`,
		d.MatchResultID, string(d.Type), d.ContentMarkdown, string(flags), d.WasEditedByUser,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("save document: %w", err)
	}
	return id, nil
}

// GetDocument returns a stored document, or ErrNotFound when it has not been
// generated yet.
func (s *PG) GetDocument(ctx context.Context, matchID int64, docType DocType) (GeneratedDocument, error) {
	var d GeneratedDocument
	var flags []byte
	var typ string
	err := s.db.QueryRow(ctx,
		`SELECT id, match_result_id, doc_type, content_markdown, fabrication_flags, was_edited, generated_at
		   FROM generated_documents WHERE match_result_id = $1 AND doc_type = $2`,
		matchID, string(docType),
	).Scan(&d.ID, &d.MatchResultID, &typ, &d.ContentMarkdown, &flags, &d.WasEditedByUser, &d.GeneratedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return GeneratedDocument{}, ErrNotFound
	}
	if err != nil {
		return GeneratedDocument{}, fmt.Errorf("get document: %w", err)
	}
	d.Type = DocType(typ)
	_ = json.Unmarshal(flags, &d.FabricationFlags)
	return d, nil
}

// UpdateDocumentContent saves a user edit and marks the document edited.
func (s *PG) UpdateDocumentContent(ctx context.Context, id int64, content string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE generated_documents SET content_markdown = $2, was_edited = TRUE WHERE id = $1`,
		id, content)
	if err != nil {
		return fmt.Errorf("update document content: %w", err)
	}
	return nil
}

// ── Selection ──

// UpsertSelection records that the user selected a match and, when opened is
// true, that its posting was opened for manual submission.
func (s *PG) UpsertSelection(ctx context.Context, matchID int64, opened bool) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO selections (match_result_id, was_posting_opened)
		 VALUES ($1, $2)
		 ON CONFLICT (match_result_id) DO UPDATE SET
		   was_posting_opened = selections.was_posting_opened OR EXCLUDED.was_posting_opened`,
		matchID, opened)
	if err != nil {
		return fmt.Errorf("upsert selection: %w", err)
	}
	return nil
}

// ── Chat ──

// AppendChatMessage stores one assistant-conversation turn.
func (s *PG) AppendChatMessage(ctx context.Context, role, content string) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO chat_messages (role, content) VALUES ($1, $2)`, role, content)
	if err != nil {
		return fmt.Errorf("append chat message: %w", err)
	}
	return nil
}

// ListChatMessages returns the most recent messages in chronological order.
func (s *PG) ListChatMessages(ctx context.Context, limit int) ([]ChatMessage, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, role, content, created_at FROM (
		   SELECT id, role, content, created_at FROM chat_messages ORDER BY id DESC LIMIT $1
		 ) recent ORDER BY id ASC`, limit)
	if err != nil {
		return nil, fmt.Errorf("list chat messages: %w", err)
	}
	defer rows.Close()
	var out []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan chat message: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ── helpers ──

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func nonNilHealth(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	return in
}

func nonNilPenalties(in map[string]int) map[string]int {
	if in == nil {
		return map[string]int{}
	}
	return in
}

func defaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func defaultWorkLocation(v WorkLocation) WorkLocation {
	if v == "" {
		return WorkUnknown
	}
	return v
}
