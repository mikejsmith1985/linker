// Package web serves the review dashboard: list drafts, edit them, approve
// (which queues to the publisher), reject, regenerate, and view history. It is
// rendered with templ and driven by HTMX fragment swaps.
package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mikejsmith1985/linker/internal/buffer"
	"github.com/mikejsmith1985/linker/internal/store"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
)

// Actions are the orchestrator-backed operations the dashboard can trigger.
type Actions interface {
	Tick(ctx context.Context) error
	Regenerate(ctx context.Context, postID int64) error
}

// Server holds dependencies for the HTTP handlers.
type Server struct {
	store   store.Store
	pub     buffer.Publisher
	actions Actions
	log     *slog.Logger
}

// NewServer builds the dashboard server. A nil logger falls back to default.
func NewServer(st store.Store, pub buffer.Publisher, actions Actions, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{store: st, pub: pub, actions: actions, log: log}
}

// Routes returns the configured HTTP handler.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", s.handleDashboard)
	r.Get("/history", s.handleHistory)
	r.Post("/poll", s.handlePoll)
	r.Post("/posts/{id}/save", s.handleSave)
	r.Post("/posts/{id}/regenerate", s.handleRegenerate)
	r.Post("/posts/{id}/reject", s.handleReject)
	r.Post("/posts/{id}/approve", s.handleApprove)
	return r
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	drafts, err := s.store.ListDrafts(r.Context())
	if err != nil {
		s.fail(w, "load drafts", err)
		return
	}
	last, err := s.store.LastQueuedAt(r.Context())
	if err != nil {
		s.fail(w, "load cadence", err)
		return
	}
	s.render(w, r, Dashboard(cadenceText(last), drafts))
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListHistory(r.Context())
	if err != nil {
		s.fail(w, "load history", err)
		return
	}
	s.render(w, r, History(items))
}

func (s *Server) handlePoll(w http.ResponseWriter, r *http.Request) {
	if err := s.actions.Tick(r.Context()); err != nil {
		s.fail(w, "poll", err)
		return
	}
	drafts, err := s.store.ListDrafts(r.Context())
	if err != nil {
		s.fail(w, "load drafts", err)
		return
	}
	s.render(w, r, DraftList(drafts))
}

func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.fail(w, "parse form", err)
		return
	}
	content := r.FormValue("content")
	hashtags := r.FormValue("hashtags")
	if err := s.store.UpdatePostContent(r.Context(), id, content, hashtags); err != nil {
		s.fail(w, "save post", err)
		return
	}
	s.renderCard(w, r, id)
}

func (s *Server) handleRegenerate(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	if err := s.actions.Regenerate(r.Context(), id); err != nil {
		s.fail(w, "regenerate", err)
		return
	}
	s.renderCard(w, r, id)
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	if err := s.store.SetPostStatus(r.Context(), id, store.StatusRejected); err != nil {
		s.fail(w, "reject", err)
		return
	}
	s.render(w, r, Removed())
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	post, err := s.store.GetPost(r.Context(), id)
	if err != nil {
		s.fail(w, "load post", err)
		return
	}
	externalID, err := s.pub.Queue(r.Context(), post)
	if err != nil {
		s.fail(w, "queue to buffer", err)
		return
	}
	if err := s.store.MarkQueued(r.Context(), id, externalID); err != nil {
		s.fail(w, "mark queued", err)
		return
	}
	view, err := s.postView(r.Context(), id)
	if err != nil {
		s.fail(w, "reload post", err)
		return
	}
	s.render(w, r, QueuedCard(view))
}

// renderCard reloads a post + its event and renders the editable card.
func (s *Server) renderCard(w http.ResponseWriter, r *http.Request, id int64) {
	view, err := s.postView(r.Context(), id)
	if err != nil {
		s.fail(w, "reload post", err)
		return
	}
	s.render(w, r, PostCard(view))
}

func (s *Server) postView(ctx context.Context, id int64) (store.PostWithEvent, error) {
	post, err := s.store.GetPost(ctx, id)
	if err != nil {
		return store.PostWithEvent{}, err
	}
	ev, err := s.store.GetEvent(ctx, post.EventID)
	if err != nil {
		return store.PostWithEvent{}, err
	}
	return store.PostWithEvent{
		Post:       post,
		Repo:       ev.Repo,
		EventType:  ev.Type,
		EventTitle: ev.Title,
		EventURL:   ev.URL,
	}, nil
}

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

// ---- template helpers ----

func cardID(id int64) string { return fmt.Sprintf("post-%d", id) }

func action(id int64, verb string) string { return fmt.Sprintf("/posts/%d/%s", id, verb) }

func cadenceText(last *time.Time) string {
	if last == nil {
		return "Nothing queued yet — a good time to ship your first post."
	}
	return "Last queued " + humanizeSince(time.Since(*last)) + " ago."
}

func queuedWhen(p store.PostWithEvent) string {
	if p.QueuedAt == nil {
		return ""
	}
	return p.QueuedAt.Format("2006-01-02 15:04")
}

func excerptText(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	const n = 160
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}

func humanizeSince(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "moments"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hr", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}

const styleCSS = `
:root { --bg:#0f1115; --card:#1a1d24; --ink:#e7e9ee; --muted:#9aa3b2; --accent:#3b82f6; --ok:#16a34a; --danger:#ef4444; }
* { box-sizing: border-box; }
body { margin:0; font-family: ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, sans-serif; background:var(--bg); color:var(--ink); }
.topbar { display:flex; align-items:center; gap:1.5rem; padding:1rem 1.5rem; border-bottom:1px solid #2a2e38; }
.brand { font-weight:700; letter-spacing:.02em; }
nav a { color:var(--muted); text-decoration:none; margin-right:1rem; }
nav a:hover { color:var(--ink); }
main { max-width:760px; margin:0 auto; padding:1.5rem; }
.cadence { display:flex; align-items:center; gap:1rem; color:var(--muted); margin-bottom:1.25rem; }
.card { background:var(--card); border:1px solid #2a2e38; border-radius:12px; padding:1rem 1.1rem; margin-bottom:1rem; }
.card.queued { opacity:.8; }
.meta { display:flex; align-items:center; gap:.6rem; flex-wrap:wrap; font-size:.9rem; color:var(--muted); margin-bottom:.6rem; }
.badge { background:#272b35; padding:.1rem .5rem; border-radius:999px; font-size:.75rem; }
.badge.ok { background:rgba(22,163,74,.18); color:#4ade80; }
.src { color:var(--accent); text-decoration:none; }
textarea { width:100%; background:#0d0f14; color:var(--ink); border:1px solid #2a2e38; border-radius:8px; padding:.7rem; font:inherit; resize:vertical; }
.tags { width:100%; margin-top:.5rem; background:#0d0f14; color:var(--ink); border:1px solid #2a2e38; border-radius:8px; padding:.5rem; font:inherit; }
.actions { display:flex; gap:.5rem; flex-wrap:wrap; margin-top:.7rem; }
button { cursor:pointer; border:none; border-radius:8px; padding:.5rem .8rem; font:inherit; }
.primary { background:var(--accent); color:#fff; }
.secondary { background:#272b35; color:var(--ink); }
.danger { background:transparent; color:var(--danger); border:1px solid var(--danger); }
.poll { background:#272b35; color:var(--ink); }
.empty, .hint { color:var(--muted); }
.history { list-style:none; padding:0; }
.history li { background:var(--card); border:1px solid #2a2e38; border-radius:12px; padding:.8rem 1rem; margin-bottom:.7rem; }
.excerpt { color:var(--muted); margin:.3rem 0 0; }
.when { margin-left:auto; }
.htmx-indicator { opacity:0; transition:opacity .2s; }
.htmx-request .htmx-indicator, .htmx-request.htmx-indicator { opacity:1; }
`
