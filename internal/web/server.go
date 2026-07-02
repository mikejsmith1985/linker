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
)

// Actions are the orchestrator-backed operations the dashboard can trigger.
type Actions interface {
	RunSearch(ctx context.Context) (int64, error)
}

// ResumeIngestor validates, parses, and stores an uploaded resume.
type ResumeIngestor interface {
	Ingest(ctx context.Context, filename, format string, data []byte) (store.Resume, error)
}

// Server holds dependencies for the HTTP handlers.
type Server struct {
	store    store.Store
	ingestor ResumeIngestor
	actions  Actions
	log      *slog.Logger
}

// maxResumeBytes bounds an uploaded resume to a sane size.
const maxResumeBytes = 10 << 20 // 10 MiB

// NewServer builds the dashboard server. A nil logger falls back to default.
func NewServer(st store.Store, ingestor ResumeIngestor, actions Actions, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{store: st, ingestor: ingestor, actions: actions, log: log}
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
	r.Get("/search/{id}", s.handleSearchResults)
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
	prefs := store.Preferences{
		RequiredSalaryMin: atoiOrZero(r.FormValue("required_salary_min")),
		SalaryCurrency:    "USD",
		WorkLocationPref:  parseWorkLocation(r.FormValue("work_location_pref")),
		WillingToTravel:   r.FormValue("willing_to_travel") != "",
		WillingToRelocate: r.FormValue("willing_to_relocate") != "",
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

func (s *Server) handleSearchResults(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
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

const styleCSS = `
:root { --bg:#0f1115; --card:#1a1d24; --ink:#e7e9ee; --muted:#9aa3b2; --accent:#3b82f6; --ok:#16a34a; --danger:#ef4444; }
* { box-sizing: border-box; }
body { margin:0; font-family: ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, sans-serif; background:var(--bg); color:var(--ink); }
.topbar { display:flex; align-items:center; gap:1.5rem; padding:1rem 1.5rem; border-bottom:1px solid #2a2e38; }
.brand { font-weight:700; letter-spacing:.02em; }
nav a { color:var(--muted); text-decoration:none; margin-right:1rem; }
nav a:hover { color:var(--ink); }
main { max-width:820px; margin:0 auto; padding:1.5rem; }
h2 { margin:0 0 .6rem; font-size:1.05rem; }
.card { background:var(--card); border:1px solid #2a2e38; border-radius:12px; padding:1rem 1.1rem; margin-bottom:1rem; }
.card.empty { text-align:center; }
.meta { display:flex; align-items:center; gap:.6rem; flex-wrap:wrap; margin-bottom:.4rem; }
.score { background:var(--accent); color:#fff; font-weight:700; border-radius:8px; padding:.15rem .55rem; }
.badge { background:#272b35; padding:.1rem .5rem; border-radius:999px; font-size:.75rem; color:var(--muted); }
.src { color:var(--accent); text-decoration:none; }
.explain { margin:.3rem 0 0; }
.hint, .empty, .health li { color:var(--muted); }
.ok { color:#4ade80; }
.health { list-style:none; padding:0; display:flex; gap:.8rem; flex-wrap:wrap; margin:0; }
label { display:block; margin:.6rem 0; }
label.check { display:flex; align-items:center; gap:.5rem; }
input[type=number], select { background:#0d0f14; color:var(--ink); border:1px solid #2a2e38; border-radius:8px; padding:.5rem; font:inherit; margin-left:.5rem; }
.actions { display:flex; gap:.5rem; flex-wrap:wrap; align-items:center; margin-top:.7rem; }
button { cursor:pointer; border:none; border-radius:8px; padding:.5rem .8rem; font:inherit; }
button:disabled { opacity:.5; cursor:not-allowed; }
.primary { background:var(--accent); color:#fff; }
.secondary { background:#272b35; color:var(--ink); }
`
