// Package web serves the single-user dashboard: upload a resume, set
// preferences, run a search, and review scored matches. Rendered with templ.
// It never submits an application anywhere — selecting a job only opens the
// original posting for the user to submit manually.
package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mikejsmith1985/linker/internal/orchestrator"
	"github.com/mikejsmith1985/linker/internal/resume"
	"github.com/mikejsmith1985/linker/internal/store"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/go-pdf/fpdf"
	"github.com/yuin/goldmark"
)

// Actions are the orchestrator-backed operations the dashboard can trigger. The
// search operations start a background search and return its id immediately.
type Actions interface {
	StartSearch(ctx context.Context) (int64, error)
	StartSearchURLs(ctx context.Context, urls []string) (int64, error)
	StartSearchCompanies(ctx context.Context, companies []string) (int64, error)
}

// ResumeIngestor validates, parses, and stores an uploaded resume.
type ResumeIngestor interface {
	Ingest(ctx context.Context, filename, format string, data []byte) (store.Resume, error)
}

// DocumentService generates (and caches) a tailored document for a match.
type DocumentService interface {
	EnsureDocument(ctx context.Context, matchID int64, docType store.DocType, opening store.JobOpening, resumeFacts string) (store.GeneratedDocument, error)
}

// Chatter answers a user's assistant message (and acts on the app).
type Chatter interface {
	Handle(ctx context.Context, userText string) (string, error)
}

// Server holds dependencies for the HTTP handlers.
type Server struct {
	store    store.Store
	ingestor ResumeIngestor
	actions  Actions
	docs     DocumentService
	chat     Chatter
	log      *slog.Logger
}

// maxResumeBytes bounds an uploaded resume to a sane size.
const maxResumeBytes = 10 << 20 // 10 MiB

// NewServer builds the dashboard server. A nil logger falls back to default.
func NewServer(st store.Store, ingestor ResumeIngestor, actions Actions, docs DocumentService, chat Chatter, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{store: st, ingestor: ingestor, actions: actions, docs: docs, chat: chat, log: log}
}

// Routes returns the configured HTTP handler.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", s.handleHome)
	r.Get("/static/styles.css", s.handleCSS)
	r.Post("/resume", s.handleUploadResume)
	r.Get("/settings", s.handleSettings)
	r.Post("/settings", s.handleSaveSettings)
	r.Post("/search", s.handleSearch)
	r.Post("/search/urls", s.handleSearchURLs)
	r.Post("/search/companies", s.handleSearchCompanies)
	r.Get("/search/{id}", s.handleSearchResults)
	r.Get("/searches", s.handleSearches)
	r.Get("/matches", s.handleMatches)
	r.Get("/assistant/panel", s.handleAssistantPanel)
	r.Post("/assistant/message", s.handleAssistantMessage)
	r.Get("/job/{id}", s.handleJob)
	r.Post("/job/{id}/review", s.handleReview)
	r.Post("/job/{id}/documents/{docType}", s.handleSaveDocument)
	r.Get("/job/{id}/documents/{docType}/download", s.handleDownloadDocument)
	r.Post("/job/{id}/select", s.handleSelect)
	r.Post("/job/{id}/open", s.handleOpenPosting)
	return r
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	resume, err := s.store.GetActiveResume(r.Context())
	hasResume := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.fail(w, "load resume", err)
		return
	}
	s.render(w, r, Home(&resume, hasResume))
}

func (s *Server) handleUploadResume(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxResumeBytes); err != nil {
		http.Error(w, "could not read upload", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("resume")
	if err != nil {
		http.Error(w, "no resume file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	format := resume.DetectFormat(header.Filename)
	if format == "" {
		http.Error(w, "unsupported file type; upload a PDF, DOCX, or TXT resume", http.StatusBadRequest)
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxResumeBytes))
	if err != nil {
		s.fail(w, "read resume", err)
		return
	}

	if _, err := s.ingestor.Ingest(r.Context(), header.Filename, format, data); err != nil {
		if errors.Is(err, resume.ErrUnreadable) {
			http.Error(w, "that resume is empty or could not be read; please upload a different file", http.StatusBadRequest)
			return
		}
		s.fail(w, "ingest resume", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	prefs, err := s.store.GetPreferences(r.Context())
	if err != nil {
		s.fail(w, "load preferences", err)
		return
	}
	s.render(w, r, Settings(prefs))
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read form", http.StatusBadRequest)
		return
	}
	ack := r.FormValue("browser_automation_ack") != ""
	enableBrowser := r.FormValue("enable_browser") != ""
	// Enabling the browser source is refused without the risk acknowledgment (FR-023).
	if enableBrowser && !ack {
		http.Error(w, "enabling browser automation requires acknowledging the terms-of-service and account-ban risk", http.StatusBadRequest)
		return
	}

	prefs := store.Preferences{
		RequiredSalaryMin:    atoiOrZero(r.FormValue("required_salary_min")),
		SalaryCurrency:       "USD",
		WorkLocationPref:     parseWorkLocation(r.FormValue("work_location_pref")),
		StrictWorkLocation:   r.FormValue("strict_work_location") != "",
		TargetRoles:          splitLines(r.FormValue("target_roles")),
		NewRolesOnly:         r.FormValue("new_roles_only") != "",
		Location:             defaultLocation(r.FormValue("location")),
		WillingToTravel:      r.FormValue("willing_to_travel") != "",
		WillingToRelocate:    r.FormValue("willing_to_relocate") != "",
		BrowserAutomationAck: ack,
	}
	if enableBrowser && ack {
		prefs.EnabledSources = []string{"browser"}
	}
	if _, err := s.store.SavePreferences(r.Context(), prefs); err != nil {
		s.fail(w, "save preferences", err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	_, err := s.actions.StartSearch(r.Context())
	if errors.Is(err, orchestrator.ErrNoResume) {
		http.Error(w, "upload a resume before searching", http.StatusBadRequest)
		return
	}
	if err != nil {
		s.fail(w, "start search", err)
		return
	}
	s.renderRecent(w, r)
}

// renderRecent renders the recent-searches activity list (the search-feedback UI).
func (s *Server) renderRecent(w http.ResponseWriter, r *http.Request) {
	summaries, err := s.store.ListRecentSearches(r.Context(), 8)
	if err != nil {
		s.fail(w, "load searches", err)
		return
	}
	s.render(w, r, RecentSearches(summaries))
}

// handleSearches serves the recent-searches list, polled while a search runs.
func (s *Server) handleSearches(w http.ResponseWriter, r *http.Request) {
	s.renderRecent(w, r)
}

// handleSearchURLs scores one or more user-pasted posting URLs (FR-021).
func (s *Server) handleSearchURLs(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read form", http.StatusBadRequest)
		return
	}
	urls := splitURLs(r.FormValue("urls"))
	if len(urls) == 0 {
		http.Error(w, "paste at least one posting URL", http.StatusBadRequest)
		return
	}
	_, err := s.actions.StartSearchURLs(r.Context(), urls)
	if errors.Is(err, orchestrator.ErrNoResume) {
		http.Error(w, "upload a resume before searching", http.StatusBadRequest)
		return
	}
	if err != nil {
		s.fail(w, "start url search", err)
		return
	}
	s.renderRecent(w, r)
}

// handleSearchCompanies scores openings pulled from named companies' ATS feeds.
func (s *Server) handleSearchCompanies(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read form", http.StatusBadRequest)
		return
	}
	companies := splitLines(r.FormValue("companies"))
	if len(companies) == 0 {
		http.Error(w, "enter at least one company name", http.StatusBadRequest)
		return
	}
	_, err := s.actions.StartSearchCompanies(r.Context(), companies)
	if errors.Is(err, orchestrator.ErrNoResume) {
		http.Error(w, "upload a resume before searching", http.StatusBadRequest)
		return
	}
	if err != nil {
		s.fail(w, "start company search", err)
		return
	}
	s.renderRecent(w, r)
}

func (s *Server) handleSearchResults(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	s.renderSearch(w, r, id)
}

// renderSearch loads a search and its qualifying matches and renders the results.
func (s *Server) renderSearch(w http.ResponseWriter, r *http.Request, id int64) {
	search, err := s.store.GetSearch(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "search not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.fail(w, "load search", err)
		return
	}
	matches, err := s.store.ListQualifying(r.Context(), id)
	if err != nil {
		s.fail(w, "load matches", err)
		return
	}
	s.render(w, r, Results(search, matches))
}

// handleAssistantPanel renders the side-ribbon assistant (thread + input),
// loaded into the layout on every page.
func (s *Server) handleAssistantPanel(w http.ResponseWriter, r *http.Request) {
	messages, err := s.store.ListChatMessages(r.Context(), 100)
	if err != nil {
		s.fail(w, "load chat", err)
		return
	}
	s.render(w, r, AssistantPanel(messages))
}

// handleAssistantMessage runs one assistant turn and returns the updated thread.
func (s *Server) handleAssistantMessage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read form", http.StatusBadRequest)
		return
	}
	message := strings.TrimSpace(r.FormValue("message"))
	if message == "" {
		http.Error(w, "type a message", http.StatusBadRequest)
		return
	}
	if _, err := s.chat.Handle(r.Context(), message); err != nil {
		s.fail(w, "assistant", err)
		return
	}
	messages, err := s.store.ListChatMessages(r.Context(), 100)
	if err != nil {
		s.fail(w, "load chat", err)
		return
	}
	s.render(w, r, ChatThread(messages))
}

// handleMatches renders the latest completed search's results, so the user can
// always get back to their list without a saved URL.
// handleMatches shows every qualifying opening ever found (de-duplicated, ranked),
// so a later empty search never hides earlier results.
func (s *Server) handleMatches(w http.ResponseWriter, r *http.Request) {
	matches, err := s.store.ListAllQualifying(r.Context())
	if err != nil {
		s.fail(w, "load matches", err)
		return
	}
	if len(matches) == 0 {
		s.render(w, r, NoMatches())
		return
	}
	// Passed roles are hidden by default so the list stays focused; a toggle
	// (?passed=1) brings them back.
	showPassed := r.URL.Query().Get("passed") == "1"
	passedCount := 0
	visible := matches[:0:0]
	for _, m := range matches {
		if m.Opening.ReviewStatus == store.ReviewPassed {
			passedCount++
			if !showPassed {
				continue
			}
		}
		visible = append(visible, m)
	}
	s.render(w, r, Matches(visible, showPassed, passedCount))
}

// handleReview persists a Pass/Interested/new mark on the opening and returns the
// updated card for an in-place swap.
func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read form", http.StatusBadRequest)
		return
	}
	status, ok := parseReviewStatus(r.FormValue("status"))
	if !ok {
		http.Error(w, "invalid review status", http.StatusBadRequest)
		return
	}
	reason := ""
	if status == store.ReviewPassed {
		reason = strings.TrimSpace(r.FormValue("reason"))
	}
	match, err := s.store.GetMatchWithOpening(r.Context(), id)
	if err != nil {
		s.fail(w, "load match", err)
		return
	}
	if err := s.store.SetOpeningReviewStatus(r.Context(), match.JobOpeningID, status, reason); err != nil {
		s.fail(w, "save review", err)
		return
	}
	match.Opening.ReviewStatus = status
	match.Opening.ReviewReason = reason
	s.render(w, r, matchCard(match))
}

func parseReviewStatus(s string) (string, bool) {
	switch s {
	case store.ReviewInterested, store.ReviewPassed, store.ReviewNew:
		return s, true
	default:
		return "", false
	}
}

// handleJob renders one opening: score, explanation, and both tailored
// documents — generating them on first view and caching thereafter (FR-007).
// Documents are only available for qualifying (>=70) openings (FR-008).
func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	match, err := s.store.GetMatchWithOpening(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "match not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.fail(w, "load match", err)
		return
	}
	if !match.IsQualifying {
		http.Error(w, "documents are only generated for qualifying matches (score 70+)", http.StatusForbidden)
		return
	}

	facts := s.resumeFacts(r.Context())
	tailored, err := s.docs.EnsureDocument(r.Context(), id, store.TailoredResume, match.Opening, facts)
	if err != nil {
		s.fail(w, "generate tailored resume", err)
		return
	}
	cover, err := s.docs.EnsureDocument(r.Context(), id, store.CoverLetter, match.Opening, facts)
	if err != nil {
		s.fail(w, "generate cover letter", err)
		return
	}
	s.render(w, r, Job(match, tailored, cover))
}

// handleSaveDocument stores a user's edit to a generated document (FR-010).
func (s *Server) handleSaveDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	docType, ok := parseDocType(chi.URLParam(r, "docType"))
	if !ok {
		http.Error(w, "unknown document type", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read form", http.StatusBadRequest)
		return
	}
	doc, err := s.store.GetDocument(r.Context(), id, docType)
	if err != nil {
		s.fail(w, "load document", err)
		return
	}
	if err := s.store.UpdateDocumentContent(r.Context(), doc.ID, r.FormValue("content")); err != nil {
		s.fail(w, "save document", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/job/%d", id), http.StatusSeeOther)
}

// handleDownloadDocument serves a generated document as txt, md, or pdf (FR-010).
func (s *Server) handleDownloadDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	docType, ok := parseDocType(chi.URLParam(r, "docType"))
	if !ok {
		http.Error(w, "unknown document type", http.StatusBadRequest)
		return
	}
	doc, err := s.store.GetDocument(r.Context(), id, docType)
	if err != nil {
		http.Error(w, "document not found", http.StatusNotFound)
		return
	}
	base := s.downloadFilenameBase(r.Context(), id, docType)
	switch r.URL.Query().Get("fmt") {
	case "pdf":
		s.writePDF(w, base, doc.ContentMarkdown)
	case "md":
		writeAttachment(w, base+".md", "text/markdown; charset=utf-8", []byte(doc.ContentMarkdown))
	default:
		writeAttachment(w, base+".txt", "text/plain; charset=utf-8", []byte(doc.ContentMarkdown))
	}
}

// downloadFilenameBase builds the "Candidate_JobTitle_DocType" base name (no
// extension) for a downloaded document so the saved file is immediately
// identifiable without renaming. A missing job title or name simply drops that
// segment rather than failing the download.
func (s *Server) downloadFilenameBase(ctx context.Context, matchID int64, docType store.DocType) string {
	jobTitle := ""
	if match, err := s.store.GetMatchWithOpening(ctx, matchID); err == nil {
		jobTitle = match.Opening.Title
	}
	return documentFilenameBase(s.resolveCandidateName(ctx), jobTitle, docType)
}

// resolveCandidateName determines the name that leads download filenames. The
// data model stores no explicit candidate name, so we infer it from the active
// resume, falling back to a generic label when no resume is present.
func (s *Server) resolveCandidateName(ctx context.Context) string {
	resume, err := s.store.GetActiveResume(ctx)
	if err == nil {
		if name := inferNameFromResume(resume); name != "" {
			return name
		}
	}
	return "Candidate"
}

// documentFilenameBase joins the candidate name, job title, and document-type
// label into a single filename-safe token (e.g. "Michael_Smith_Staff_Engineer_Resume").
// Empty segments are omitted so the result never contains doubled or trailing separators.
func documentFilenameBase(candidateName, jobTitle string, docType store.DocType) string {
	segments := make([]string, 0, 3)
	for _, raw := range []string{candidateName, jobTitle, docTypeFilenameLabel(docType)} {
		if token := sanitizeFilenameSegment(raw); token != "" {
			segments = append(segments, token)
		}
	}
	if len(segments) == 0 {
		return string(docType) // defensive: should never happen since the label is always present
	}
	return strings.Join(segments, "_")
}

// docTypeFilenameLabel is the concise document kind used in download filenames
// ("Resume"/"Cover Letter") — distinct from docTypeLabel, which titles the UI.
func docTypeFilenameLabel(docType store.DocType) string {
	switch docType {
	case store.CoverLetter:
		return "Cover Letter"
	case store.TailoredResume:
		return "Resume"
	default:
		return string(docType)
	}
}

// sanitizeFilenameSegment reduces a string to a filename-safe token: runs of
// characters that are neither letters nor digits collapse to a single
// underscore, and leading/trailing underscores are trimmed. Hyphens are kept as
// word joiners (e.g. "Mary-Jane") rather than dropped.
func sanitizeFilenameSegment(raw string) string {
	var builder strings.Builder
	wasSeparator := false
	for _, char := range strings.TrimSpace(raw) {
		switch {
		case unicode.IsLetter(char) || unicode.IsDigit(char):
			builder.WriteRune(char)
			wasSeparator = false
		case char == '-':
			builder.WriteRune('-')
			wasSeparator = false
		case !wasSeparator:
			builder.WriteRune('_')
			wasSeparator = true
		}
	}
	return strings.Trim(builder.String(), "_-")
}

// inferNameFromResume guesses the candidate's name from a resume. Résumés almost
// always lead with the person's name, so we take the first short, name-shaped
// line of the extracted text, then fall back to the uploaded file's base name.
func inferNameFromResume(resume store.Resume) string {
	const maxLinesToScan = 6
	scanned := 0
	for _, line := range strings.Split(resume.RawText, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if looksLikePersonName(trimmed) {
			return trimmed
		}
		scanned++
		if scanned >= maxLinesToScan {
			break
		}
	}
	return nameFromFilename(resume.OriginalFilename)
}

// looksLikePersonName reports whether a line is plausibly a person's full name:
// two to four words of letters (allowing hyphens, apostrophes, and initials),
// with no digits or email markers that would signal a contact line instead.
func looksLikePersonName(line string) bool {
	if strings.ContainsAny(line, "@0123456789") {
		return false
	}
	words := strings.Fields(line)
	if len(words) < 2 || len(words) > 4 {
		return false
	}
	for _, word := range words {
		if resumeHeadingWords[strings.ToLower(word)] {
			return false // a section heading like "SUMMARY OF QUALIFICATIONS", not a name
		}
		for _, char := range word {
			if !unicode.IsLetter(char) && char != '-' && char != '\'' && char != '.' {
				return false
			}
		}
	}
	return true
}

// resumeHeadingWords are words that appear in resume section headers but never in
// a person's name; their presence tells us a line is a heading, not the name.
var resumeHeadingWords = map[string]bool{
	"summary": true, "qualifications": true, "objective": true, "profile": true,
	"experience": true, "education": true, "skills": true, "contact": true,
	"references": true, "employment": true, "history": true, "professional": true,
	"resume": true, "curriculum": true, "vitae": true, "of": true, "and": true, "the": true,
}

// nameFromFilename recovers a name from an uploaded resume's filename by
// stripping the extension and common non-name words (resume, cv, cover, letter)
// and treating separators as spaces (e.g. "Michael_Smith_Resume.pdf" → "Michael Smith").
func nameFromFilename(filename string) string {
	base := filename
	if dot := strings.LastIndex(base, "."); dot > 0 {
		base = base[:dot]
	}
	base = strings.Map(func(char rune) rune {
		switch char {
		case '_', '-', '.', '(', ')':
			return ' '
		}
		return char
	}, base)
	kept := make([]string, 0, 4)
	for _, word := range strings.Fields(base) {
		switch strings.ToLower(word) {
		case "resume", "cv", "cover", "letter":
			continue
		}
		if !containsLetter(word) {
			continue // drop dedup suffixes like the "1" in "Michael Smith (1)"
		}
		kept = append(kept, word)
	}
	return strings.Join(kept, " ")
}

// containsLetter reports whether a word has at least one letter, used to discard
// purely numeric or symbolic filename fragments when inferring a name.
func containsLetter(word string) bool {
	for _, char := range word {
		if unicode.IsLetter(char) {
			return true
		}
	}
	return false
}

// handleSelect records that the user wants to pursue this opening (FR-011).
func (s *Server) handleSelect(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	if err := s.store.UpsertSelection(r.Context(), id, false); err != nil {
		s.fail(w, "record selection", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/job/%d", id), http.StatusSeeOther)
}

// handleOpenPosting records that the posting was opened for manual submission
// and redirects the user to the original posting. The system submits nothing on
// the user's behalf (FR-012, FR-013, SC-006) — it only records and redirects.
func (s *Server) handleOpenPosting(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	match, err := s.store.GetMatchWithOpening(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "match not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.fail(w, "load match", err)
		return
	}
	if strings.TrimSpace(match.Opening.OriginalURL) == "" {
		http.Error(w, "this posting is no longer reachable", http.StatusGone)
		return
	}
	if err := s.store.UpsertSelection(r.Context(), id, true); err != nil {
		s.fail(w, "record open", err)
		return
	}
	http.Redirect(w, r, match.Opening.OriginalURL, http.StatusSeeOther)
}

// resumeFacts returns the active resume's raw text (the no-fabrication source),
// or empty when no resume is active.
func (s *Server) resumeFacts(ctx context.Context) string {
	resume, err := s.store.GetActiveResume(ctx)
	if err != nil {
		return ""
	}
	return resume.RawText
}

// PDF layout constants — sizes are in points (font) or millimeters (geometry).
const (
	pdfMarginMM     = 18.0 // page margin on all four sides
	pdfBodyPt       = 10.5 // body-text font size
	pdfLineHeight   = 5.0  // millimeters advanced per text line
	pdfBulletIndent = 6.0  // hanging indent for wrapped bullet lines
)

// writePDF renders a generated document's Markdown into a formatted PDF and
// streams it as an attachment. Unlike a raw text dump, it turns Markdown into
// real typography (headings, bold, bullet lists) and maps UTF-8 punctuation into
// the core font's encoding so em dashes and middots render correctly.
func (s *Server) writePDF(w http.ResponseWriter, base, markdown string) {
	pdf := fpdf.New("P", "mm", "Letter", "")
	pdf.SetMargins(pdfMarginMM, pdfMarginMM, pdfMarginMM)
	pdf.SetAutoPageBreak(true, pdfMarginMM)
	pdf.AddPage()
	pdf.SetFont("Helvetica", "", pdfBodyPt)
	// The core Helvetica font speaks Windows-1252, not UTF-8. We first fold the
	// few common symbols that Windows-1252 lacks (arrows, ellipsis) down to ASCII
	// so they don't silently vanish, then translate the rest — this replaces the
	// "â€"/Â·" mojibake a raw UTF-8 write would produce with correct glyphs.
	toWindows1252 := pdf.UnicodeTranslatorFromDescriptor("cp1252")
	translate := func(text string) string { return toWindows1252(normalizeForPDF(text)) }
	renderMarkdownToPDF(pdf, markdown, translate)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+base+".pdf\"")
	if err := pdf.Output(w); err != nil {
		s.log.Error("pdf output failed", "err", err)
	}
}

// pdfSymbolFolds maps Unicode symbols that Windows-1252 cannot represent to
// ASCII fallbacks, so they render as sensible characters instead of being
// dropped by the font encoder (e.g. the "→" in "QA Lead → Architect").
var pdfSymbolFolds = strings.NewReplacer(
	"→", "-", "←", "-", "↔", "-", "⇒", "=>", "⟶", "-", "➔", "-", "➜", "-",
	"…", "...", "•", "•", "▪", "•", "◦", "•", "‣", "•",
	"✓", "-", "✔", "-", "★", "*", "☆", "*",
	" ", " ", " ", " ", " ", " ", "​", "",
)

// normalizeForPDF folds unsupported symbols to ASCII before font encoding.
func normalizeForPDF(text string) string {
	return pdfSymbolFolds.Replace(text)
}

// renderMarkdownToPDF walks the Markdown line by line and emits the matching
// PDF block: headings, horizontal rules, bullet items, blank-line spacing, or a
// normal paragraph. Inline emphasis (**bold**, *italic*) is honored within each.
func renderMarkdownToPDF(pdf *fpdf.Fpdf, markdown string, translate func(string) string) {
	for _, rawLine := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(rawLine)
		switch {
		case trimmed == "":
			pdf.Ln(pdfLineHeight * 0.6)
		case isHorizontalRule(trimmed):
			drawHorizontalRule(pdf)
		case strings.HasPrefix(trimmed, "#"):
			renderHeading(pdf, trimmed, translate)
		case isBulletLine(trimmed):
			renderBullet(pdf, trimmed, translate)
		default:
			renderInlineRuns(pdf, trimmed, translate)
			pdf.Ln(pdfLineHeight)
		}
	}
}

// renderHeading emits a Markdown ATX heading (#..######) as bold text sized by
// its level, with a little breathing room above it.
func renderHeading(pdf *fpdf.Fpdf, line string, translate func(string) string) {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	text := stripInlineMarkers(strings.TrimSpace(line[level:]))
	size := pdfHeadingSize(level)
	pdf.Ln(pdfLineHeight * 0.4)
	pdf.SetFont("Helvetica", "B", size)
	pdf.MultiCell(0, size*0.5, translate(text), "", "L", false)
	pdf.SetFont("Helvetica", "", pdfBodyPt)
}

// pdfHeadingSize maps a heading level (1 = biggest) to a font size in points.
func pdfHeadingSize(level int) float64 {
	switch level {
	case 1:
		return 17
	case 2:
		return 13.5
	case 3:
		return 11.5
	default:
		return pdfBodyPt + 0.5
	}
}

// renderBullet emits a "- " / "* " list item as a real bullet glyph with a
// hanging indent so wrapped continuation lines line up under the text.
func renderBullet(pdf *fpdf.Fpdf, line string, translate func(string) string) {
	content := strings.TrimSpace(line[1:])
	startX := pdf.GetX()
	pdf.SetFont("Helvetica", "", pdfBodyPt)
	pdf.Write(pdfLineHeight, translate("•  "))
	pdf.SetLeftMargin(startX + pdfBulletIndent)
	renderInlineRuns(pdf, content, translate)
	pdf.Ln(pdfLineHeight)
	pdf.SetLeftMargin(startX)
	pdf.SetX(startX)
}

// renderInlineRuns writes a line of inline Markdown, switching the font weight
// for **bold** and *italic* spans. fpdf's Write continues at the current
// position and wraps automatically at the right margin.
func renderInlineRuns(pdf *fpdf.Fpdf, text string, translate func(string) string) {
	for _, run := range parseInlineRuns(text) {
		style := ""
		if run.isBold {
			style += "B"
		}
		if run.isItalic {
			style += "I"
		}
		pdf.SetFont("Helvetica", style, pdfBodyPt)
		pdf.Write(pdfLineHeight, translate(run.text))
	}
	pdf.SetFont("Helvetica", "", pdfBodyPt)
}

// inlineRun is a stretch of text sharing one emphasis style.
type inlineRun struct {
	text     string
	isBold   bool
	isItalic bool
}

// parseInlineRuns splits inline Markdown into styled runs by toggling on ** and
// * markers. Backticks are dropped (inline code renders as plain text). Unbalanced
// markers simply leave the style toggled — good enough for resume/cover-letter copy.
func parseInlineRuns(text string) []inlineRun {
	var runs []inlineRun
	var buffer strings.Builder
	isBold, isItalic := false, false
	flush := func() {
		if buffer.Len() > 0 {
			runs = append(runs, inlineRun{text: buffer.String(), isBold: isBold, isItalic: isItalic})
			buffer.Reset()
		}
	}
	chars := []rune(text)
	for i := 0; i < len(chars); i++ {
		switch {
		case chars[i] == '*' && i+1 < len(chars) && chars[i+1] == '*':
			flush()
			isBold = !isBold
			i++ // consume the second '*'
		case chars[i] == '*':
			flush()
			isItalic = !isItalic
		case chars[i] == '`':
			// drop inline-code backticks
		default:
			buffer.WriteRune(chars[i])
		}
	}
	flush()
	if len(runs) == 0 {
		runs = append(runs, inlineRun{text: text})
	}
	return runs
}

// stripInlineMarkers removes emphasis markers, used for headings where we apply
// styling structurally rather than parsing inline runs.
func stripInlineMarkers(text string) string {
	return strings.NewReplacer("**", "", "*", "", "`", "").Replace(text)
}

// isHorizontalRule reports whether a line is a Markdown thematic break
// (three or more -, *, or _ characters and nothing else).
func isHorizontalRule(line string) bool {
	if len(line) < 3 {
		return false
	}
	for _, char := range line {
		if char != '-' && char != '*' && char != '_' {
			return false
		}
	}
	return true
}

// isBulletLine reports whether a line begins an unordered list item.
func isBulletLine(line string) bool {
	return strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "• ")
}

// drawHorizontalRule draws a thin grey divider across the text column.
func drawHorizontalRule(pdf *fpdf.Fpdf) {
	pdf.Ln(pdfLineHeight * 0.3)
	posY := pdf.GetY()
	left, _, right, _ := pdf.GetMargins()
	pageWidth, _ := pdf.GetPageSize()
	pdf.SetDrawColor(190, 190, 190)
	pdf.Line(left, posY, pageWidth-right, posY)
	pdf.Ln(pdfLineHeight * 0.7)
}

func writeAttachment(w http.ResponseWriter, filename, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	_, _ = w.Write(body)
}

func parseDocType(s string) (store.DocType, bool) {
	switch s {
	case string(store.TailoredResume):
		return store.TailoredResume, true
	case string(store.CoverLetter):
		return store.CoverLetter, true
	default:
		return "", false
	}
}

func (s *Server) handleCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = io.WriteString(w, styleCSS)
}

// ── helpers ──

func (s *Server) idParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		s.log.Error("render failed", "err", err)
	}
}

func (s *Server) fail(w http.ResponseWriter, what string, err error) {
	s.log.Error("handler error", "op", what, "err", err)
	http.Error(w, what+": "+err.Error(), http.StatusInternalServerError)
}

func atoiOrZero(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func defaultLocation(s string) string {
	if strings.TrimSpace(s) == "" {
		return "United States"
	}
	return strings.TrimSpace(s)
}

func parseWorkLocation(s string) store.WorkLocation {
	switch s {
	case "onsite":
		return store.WorkOnsite
	case "hybrid":
		return store.WorkHybrid
	case "remote":
		return store.WorkRemote
	default:
		return store.WorkRemote
	}
}

// ── templ view helpers ──

func intAttr(n int) string { return strconv.Itoa(n) }

func scoreText(n int) string { return strconv.Itoa(n) }

func salaryText(o store.JobOpening) string {
	switch {
	case o.SalaryMin > 0 && o.SalaryMax > 0:
		return fmt.Sprintf("$%d–$%d", o.SalaryMin, o.SalaryMax)
	case o.SalaryMax > 0:
		return fmt.Sprintf("up to $%d", o.SalaryMax)
	case o.SalaryMin > 0:
		return fmt.Sprintf("from $%d", o.SalaryMin)
	default:
		return "salary not stated"
	}
}

func sourcesText(o store.JobOpening) string {
	if len(o.SourceNames) == 0 {
		return "unknown source"
	}
	return strings.Join(o.SourceNames, ", ")
}

func jobPath(matchID int64) string { return fmt.Sprintf("/job/%d", matchID) }

func searchPath(searchID int64) string { return fmt.Sprintf("/search/%d", searchID) }

func intText(id int64) string { return strconv.FormatInt(id, 10) }

// anyRunning reports whether any listed search is still in progress (drives the
// auto-refresh poll on the recent-searches list).
func anyRunning(summaries []store.SearchSummary) bool {
	for _, sum := range summaries {
		if sum.Status == store.SearchRunning {
			return true
		}
	}
	return false
}

func searchStatusClass(status store.SearchStatus) string {
	switch status {
	case store.SearchCompleted:
		return "ok-badge"
	case store.SearchFailed:
		return "fail-badge"
	default:
		return "run-badge"
	}
}

// searchTiming describes how long a search took (or has been running) and how
// long ago it finished, so the user sees that it actually executed.
func searchTiming(sum store.SearchSummary) string {
	switch sum.Status {
	case store.SearchRunning:
		return "running for " + humanDuration(time.Since(sum.StartedAt))
	case store.SearchCompleted:
		if sum.FinishedAt != nil {
			return fmt.Sprintf("completed in %s · %s ago",
				humanDuration(sum.FinishedAt.Sub(sum.StartedAt)), humanDuration(time.Since(*sum.FinishedAt)))
		}
		return "completed"
	case store.SearchFailed:
		return "failed"
	}
	return string(sum.Status)
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

func matchCountText(count int) string {
	if count == 1 {
		return "1 match"
	}
	return fmt.Sprintf("%d matches", count)
}

func docSavePath(matchID int64, docType store.DocType) string {
	return fmt.Sprintf("/job/%d/documents/%s", matchID, docType)
}

func docDownloadPath(matchID int64, docType store.DocType, format string) string {
	return fmt.Sprintf("/job/%d/documents/%s/download?fmt=%s", matchID, docType, format)
}

func docTypeLabel(docType store.DocType) string {
	if docType == store.CoverLetter {
		return "Cover letter"
	}
	return "Tailored resume"
}

func joinFlags(flags []string) string { return strings.Join(flags, ", ") }

func joinLines(items []string) string { return strings.Join(items, "\n") }

// markdownRenderer converts document Markdown to HTML for a formatted preview.
// Raw HTML in the source is escaped (goldmark's default), so LLM-generated
// content can't inject markup.
var markdownRenderer = goldmark.New()

// renderMarkdown returns the HTML rendering of Markdown, safe to embed.
func renderMarkdown(md string) templ.Component {
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(md), &buf); err != nil {
		return templ.Raw("<p>" + templ.EscapeString(md) + "</p>")
	}
	return templ.Raw(buf.String())
}

func bubbleClass(role string) string {
	if role == "assistant" {
		return "assistant"
	}
	return "user"
}

// careersSearchURL builds a Google search that surfaces the employer's own
// careers-page posting for a role — the best place to apply for a direct
// employer, bypassing aggregator Quick-Apply.
func careersSearchURL(employer, title string) string {
	q := url.QueryEscape(fmt.Sprintf("%s careers %s", strings.TrimSpace(employer), strings.TrimSpace(title)))
	return "https://www.google.com/search?q=" + q
}

// linkedInSearchURL builds a LinkedIn jobs search for the role at the employer.
func linkedInSearchURL(employer, title string) string {
	q := url.QueryEscape(strings.TrimSpace(title + " " + employer))
	return "https://www.linkedin.com/jobs/search/?keywords=" + q
}

// reviewClass maps a review status to a CSS class for styling the card.
func reviewClass(status string) string {
	switch status {
	case store.ReviewPassed:
		return "reviewed-passed"
	case store.ReviewInterested:
		return "reviewed-interested"
	default:
		return ""
	}
}

// browserEnabled reports whether the browser source is in the enabled set.
func browserEnabled(prefs store.Preferences) bool {
	for _, name := range prefs.EnabledSources {
		if name == "browser" {
			return true
		}
	}
	return false
}

// splitURLs parses a textarea of pasted URLs separated by newlines, spaces, or
// commas into a clean list.
func splitURLs(raw string) []string {
	return splitOn(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ' ' || r == '\t' || r == ','
	})
}

// splitLines parses a textarea into entries separated by newlines or commas only,
// preserving spaces within an entry (e.g. a multi-word company name).
func splitLines(raw string) []string {
	return splitOn(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ','
	})
}

func splitOn(raw string, isSep func(rune) bool) []string {
	fields := strings.FieldsFunc(raw, isSep)
	var out []string
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

const styleCSS = `
:root { --bg:#0f1115; --card:#1a1d24; --ink:#e7e9ee; --muted:#9aa3b2; --accent:#3b82f6; --ok:#16a34a; --danger:#ef4444; --star:#f59e0b; }
* { box-sizing: border-box; }
html { font-size: 18px; }
body { margin:0; font-family: ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, sans-serif; background:var(--bg); color:var(--ink); line-height:1.5; }
.topbar { display:flex; align-items:center; gap:2rem; padding:1rem 2rem; border-bottom:1px solid #2a2e38; }
.brand { font-weight:700; letter-spacing:.02em; font-size:1.25rem; }
nav a { color:var(--muted); text-decoration:none; margin-right:1.25rem; font-size:1.05rem; }
nav a:hover { color:var(--ink); }
.shell { display:flex; gap:1.5rem; align-items:flex-start; max-width:2400px; margin:0 auto; padding:1.5rem 2rem; }
main { flex:1; min-width:0; }
.ribbon { width:400px; flex:none; position:sticky; top:1.5rem; max-height:calc(100vh - 6rem); display:flex; flex-direction:column; background:var(--card); border:1px solid #2a2e38; border-radius:14px; padding:1rem; }
.ribbon-head { display:flex; flex-direction:column; gap:.1rem; margin-bottom:.5rem; }
.ribbon-chat { flex:1; max-height:none; }
@media (max-width: 1100px) { .shell { flex-direction:column; } .ribbon { width:100%; position:static; max-height:none; } }
.doc-preview { background:#0d0f14; border:1px solid #2a2e38; border-radius:10px; padding:1rem 1.4rem; margin:.6rem 0; }
.doc-preview h1, .doc-preview h2, .doc-preview h3 { margin:.6rem 0 .3rem; }
.doc-preview ul, .doc-preview ol { padding-left:1.4rem; }
.doc-preview p { margin:.4rem 0; }
details.doc-edit { margin:.4rem 0; }
details.doc-edit summary { cursor:pointer; color:var(--accent); margin-bottom:.4rem; }
h2 { margin:0 0 .8rem; font-size:1.35rem; }
.card { background:var(--card); border:1px solid #2a2e38; border-radius:14px; padding:1.4rem 1.6rem; margin-bottom:1.25rem; }
.card.empty { text-align:center; }
.match { transition: opacity .2s, border-color .2s; }
.reviewed-interested { border-color: var(--star); box-shadow: 0 0 0 1px rgba(245,158,11,.25); }
.reviewed-passed { opacity:.5; }
.meta { display:flex; align-items:center; gap:.7rem; flex-wrap:wrap; margin-bottom:.5rem; }
.job-title { font-size:1.3rem; }
.employer { font-size:1.1rem; color:var(--muted); }
.score { background:var(--accent); color:#fff; font-weight:700; border-radius:10px; padding:.25rem .7rem; font-size:1.15rem; }
.badge { background:#272b35; padding:.2rem .65rem; border-radius:999px; font-size:.8rem; color:var(--muted); }
.badge.star { background:rgba(245,158,11,.18); color:var(--star); }
.badge.passed-tag { background:#2a2e38; color:var(--muted); }
.src { color:var(--accent); text-decoration:none; }
.explain { margin:.5rem 0 0; font-size:1.02rem; }
.hint, .empty, .health li { color:var(--muted); }
.ok { color:#4ade80; }
.health { list-style:none; padding:0; display:flex; gap:1rem; flex-wrap:wrap; margin:0; font-size:1rem; }
label { display:block; margin:.8rem 0; font-size:1.05rem; }
label.check { display:flex; align-items:center; gap:.5rem; }
input[type=number], input[type=text], select { background:#0d0f14; color:var(--ink); border:1px solid #2a2e38; border-radius:8px; padding:.6rem; font:inherit; margin-left:.5rem; }
.actions { display:flex; gap:.6rem; flex-wrap:wrap; align-items:center; margin-top:1rem; }
button { cursor:pointer; border:none; border-radius:9px; padding:.6rem 1rem; font:inherit; font-size:1rem; }
button:disabled { opacity:.5; cursor:not-allowed; }
.primary { background:var(--accent); color:#fff; }
.secondary { background:#272b35; color:var(--ink); }
.ghost { background:transparent; color:var(--muted); border:1px solid #2a2e38; }
.ghost:hover { color:var(--ink); }
.pass-select { margin-left:0; background:transparent; color:var(--muted); border:1px solid #2a2e38; border-radius:9px; padding:.55rem .8rem; font-size:1rem; }
.started { background:rgba(59,130,246,.12); border:1px solid var(--accent); border-radius:10px; padding:.8rem 1rem; margin-bottom:1rem; }
.started a { color:var(--accent); }
.chat { display:flex; flex-direction:column; gap:.7rem; max-height:60vh; overflow-y:auto; margin:1rem 0; padding:.3rem; }
.bubble { max-width:80%; padding:.6rem .9rem; border-radius:12px; }
.bubble.user { align-self:flex-end; background:var(--accent); color:#fff; }
.bubble.assistant { align-self:flex-start; background:#272b35; }
.bubble-role { display:block; font-size:.72rem; opacity:.6; margin-bottom:.2rem; text-transform:capitalize; }
.bubble-body { white-space:pre-wrap; }
.chat-form { display:flex; gap:.6rem; align-items:flex-end; }
.chat-form textarea { flex:1; }
.search-activity { list-style:none; padding:0; margin:.4rem 0 0; display:flex; flex-direction:column; gap:.5rem; }
.search-activity li { display:flex; align-items:center; gap:.7rem; flex-wrap:wrap; }
.run-badge { background:rgba(59,130,246,.18); color:#8ab4ff; border:1px solid var(--accent); }
.ok-badge { background:rgba(52,199,89,.15); color:#5fd67e; border:1px solid #2e7d43; }
.fail-badge { background:rgba(255,99,99,.15); color:#ff8a8a; border:1px solid #7d2e2e; }
.btn { display:inline-block; text-decoration:none; border-radius:9px; padding:.6rem 1rem; font-size:1rem; }
a.primary.btn { color:#fff; }
a.secondary.btn { color:var(--ink); }
textarea { width:100%; background:#0d0f14; color:var(--ink); border:1px solid #2a2e38; border-radius:8px; padding:.8rem; font:ui-monospace,monospace; font-size:.95rem; resize:vertical; }
.warn { background:rgba(239,68,68,.12); border:1px solid var(--danger); color:#fca5a5; padding:.8rem 1rem; border-radius:8px; }
.danger-zone { border:1px solid var(--danger); border-radius:8px; padding:.7rem 1rem; margin:1.2rem 0; }
.danger-zone legend { color:var(--danger); padding:0 .4rem; }
`
