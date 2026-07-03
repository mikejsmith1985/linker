// Package web serves the single-user dashboard: upload a resume, set
// preferences, run a search, and review scored matches. Rendered with templ.
// It never submits an application anywhere — selecting a job only opens the
// original posting for the user to submit manually.
package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/mikejsmith1985/linker/internal/orchestrator"
	"github.com/mikejsmith1985/linker/internal/resume"
	"github.com/mikejsmith1985/linker/internal/store"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/go-pdf/fpdf"
)

// Actions are the orchestrator-backed operations the dashboard can trigger.
type Actions interface {
	RunSearch(ctx context.Context) (int64, error)
	RunSearchURLs(ctx context.Context, urls []string) (int64, error)
	RunSearchCompanies(ctx context.Context, companies []string) (int64, error)
}

// ResumeIngestor validates, parses, and stores an uploaded resume.
type ResumeIngestor interface {
	Ingest(ctx context.Context, filename, format string, data []byte) (store.Resume, error)
}

// DocumentService generates (and caches) a tailored document for a match.
type DocumentService interface {
	EnsureDocument(ctx context.Context, matchID int64, docType store.DocType, opening store.JobOpening, resumeFacts string) (store.GeneratedDocument, error)
}

// Server holds dependencies for the HTTP handlers.
type Server struct {
	store    store.Store
	ingestor ResumeIngestor
	actions  Actions
	docs     DocumentService
	log      *slog.Logger
}

// maxResumeBytes bounds an uploaded resume to a sane size.
const maxResumeBytes = 10 << 20 // 10 MiB

// NewServer builds the dashboard server. A nil logger falls back to default.
func NewServer(st store.Store, ingestor ResumeIngestor, actions Actions, docs DocumentService, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{store: st, ingestor: ingestor, actions: actions, docs: docs, log: log}
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
	r.Get("/matches", s.handleMatches)
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
	searchID, err := s.actions.RunSearch(r.Context())
	if errors.Is(err, orchestrator.ErrNoResume) {
		http.Error(w, "upload a resume before searching", http.StatusBadRequest)
		return
	}
	if err != nil {
		s.fail(w, "run search", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/search/%d", searchID), http.StatusSeeOther)
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
	searchID, err := s.actions.RunSearchURLs(r.Context(), urls)
	if errors.Is(err, orchestrator.ErrNoResume) {
		http.Error(w, "upload a resume before searching", http.StatusBadRequest)
		return
	}
	if err != nil {
		s.fail(w, "run url search", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/search/%d", searchID), http.StatusSeeOther)
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
	searchID, err := s.actions.RunSearchCompanies(r.Context(), companies)
	if errors.Is(err, orchestrator.ErrNoResume) {
		http.Error(w, "upload a resume before searching", http.StatusBadRequest)
		return
	}
	if err != nil {
		s.fail(w, "run company search", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/search/%d", searchID), http.StatusSeeOther)
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

// handleMatches renders the latest completed search's results, so the user can
// always get back to their list without a saved URL.
func (s *Server) handleMatches(w http.ResponseWriter, r *http.Request) {
	id, err := s.store.LatestCompletedSearchID(r.Context())
	if errors.Is(err, store.ErrNotFound) {
		s.render(w, r, NoMatches())
		return
	}
	if err != nil {
		s.fail(w, "load latest search", err)
		return
	}
	s.renderSearch(w, r, id)
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
	base := fmt.Sprintf("%s-%d", docType, id)
	switch r.URL.Query().Get("fmt") {
	case "pdf":
		s.writePDF(w, base, doc.ContentMarkdown)
	case "md":
		writeAttachment(w, base+".md", "text/markdown; charset=utf-8", []byte(doc.ContentMarkdown))
	default:
		writeAttachment(w, base+".txt", "text/plain; charset=utf-8", []byte(doc.ContentMarkdown))
	}
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

func (s *Server) writePDF(w http.ResponseWriter, base, markdown string) {
	pdf := fpdf.New("P", "mm", "Letter", "")
	pdf.SetMargins(20, 20, 20)
	pdf.AddPage()
	pdf.SetFont("Helvetica", "", 11)
	for _, line := range strings.Split(markdown, "\n") {
		// MultiCell wraps long lines; an empty line becomes vertical space.
		if strings.TrimSpace(line) == "" {
			pdf.Ln(4)
			continue
		}
		pdf.MultiCell(0, 5, line, "", "", false)
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+base+".pdf\"")
	if err := pdf.Output(w); err != nil {
		s.log.Error("pdf output failed", "err", err)
	}
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
main { max-width:1200px; margin:0 auto; padding:2rem 2.5rem; }
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
.btn { display:inline-block; text-decoration:none; border-radius:9px; padding:.6rem 1rem; font-size:1rem; }
a.primary.btn { color:#fff; }
a.secondary.btn { color:var(--ink); }
textarea { width:100%; background:#0d0f14; color:var(--ink); border:1px solid #2a2e38; border-radius:8px; padding:.8rem; font:ui-monospace,monospace; font-size:.95rem; resize:vertical; }
.warn { background:rgba(239,68,68,.12); border:1px solid var(--danger); color:#fca5a5; padding:.8rem 1rem; border-radius:8px; }
.danger-zone { border:1px solid var(--danger); border-radius:8px; padding:.7rem 1rem; margin:1.2rem 0; }
.danger-zone legend { color:var(--danger); padding:0 .4rem; }
`
