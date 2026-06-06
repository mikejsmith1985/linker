// Package orchestrator ties the pieces together: on each tick it polls every
// tracked repo, persists new (de-duplicated) activity, and asks Claude to draft
// a LinkedIn post for each genuinely new event. Drafts are stored for review;
// nothing is published automatically.
package orchestrator

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mikejsmith1985/linker/internal/claude"
	"github.com/mikejsmith1985/linker/internal/github"
	"github.com/mikejsmith1985/linker/internal/store"
)

// Orchestrator coordinates polling, drafting, and persistence.
type Orchestrator struct {
	store   store.Store
	source  github.ActivitySource
	drafter claude.Drafter
	repos   []string
	log     *slog.Logger
}

// New builds an Orchestrator. A nil logger falls back to slog.Default().
func New(st store.Store, src github.ActivitySource, drafter claude.Drafter, repos []string, log *slog.Logger) *Orchestrator {
	if log == nil {
		log = slog.Default()
	}
	return &Orchestrator{store: st, source: src, drafter: drafter, repos: repos, log: log}
}

// Tick polls all tracked repos once. A failure on one repo is logged and does
// not stop the others.
func (o *Orchestrator) Tick(ctx context.Context) error {
	for _, repo := range o.repos {
		if err := o.pollRepo(ctx, repo); err != nil {
			o.log.Error("poll repo failed", "repo", repo, "err", err)
		}
	}
	return nil
}

func (o *Orchestrator) pollRepo(ctx context.Context, repo string) error {
	cur, err := o.store.GetCursor(ctx, repo)
	if err != nil {
		return fmt.Errorf("get cursor: %w", err)
	}

	res, err := o.source.Poll(ctx, repo, cur)
	if err != nil {
		return fmt.Errorf("poll: %w", err)
	}

	for _, ev := range res.Events {
		id, inserted, err := o.store.InsertEvent(ctx, ev)
		if err != nil {
			o.log.Error("insert event failed", "repo", repo, "ref", ev.Ref, "err", err)
			continue
		}
		if !inserted {
			continue // already seen — dedup
		}
		ev.ID = id
		if err := o.draftForEvent(ctx, ev, res.RepoDescription, res.ReadmeExcerpt); err != nil {
			o.log.Error("draft failed", "repo", repo, "ref", ev.Ref, "err", err)
		}
	}

	if err := o.store.SaveCursor(ctx, res.Cursor); err != nil {
		return fmt.Errorf("save cursor: %w", err)
	}
	return nil
}

func (o *Orchestrator) draftForEvent(ctx context.Context, ev store.Event, repoDesc, readmeExcerpt string) error {
	out, err := o.drafter.Draft(ctx, inputFromEvent(ev, repoDesc, readmeExcerpt))
	if err != nil {
		return err
	}
	if _, err := o.store.CreatePost(ctx, store.Post{
		EventID:  ev.ID,
		Content:  out.Content,
		Hashtags: out.Hashtags,
		Status:   store.StatusDraft,
	}); err != nil {
		return fmt.Errorf("create post: %w", err)
	}
	o.log.Info("drafted post", "repo", ev.Repo, "type", ev.Type, "ref", ev.Ref)
	return nil
}

// Regenerate re-drafts an existing post from its source event, replacing the
// post content in place. Used by the dashboard's "Regenerate" action.
func (o *Orchestrator) Regenerate(ctx context.Context, postID int64) error {
	post, err := o.store.GetPost(ctx, postID)
	if err != nil {
		return fmt.Errorf("get post: %w", err)
	}
	ev, err := o.store.GetEvent(ctx, post.EventID)
	if err != nil {
		return fmt.Errorf("get event: %w", err)
	}
	out, err := o.drafter.Draft(ctx, inputFromEvent(ev, "", ""))
	if err != nil {
		return fmt.Errorf("redraft: %w", err)
	}
	return o.store.UpdatePostContent(ctx, postID, out.Content, out.Hashtags)
}

func inputFromEvent(ev store.Event, repoDesc, readmeExcerpt string) claude.DraftInput {
	return claude.DraftInput{
		Repo:            ev.Repo,
		RepoDescription: repoDesc,
		EventType:       ev.Type,
		Title:           ev.Title,
		Body:            ev.Body,
		URL:             ev.URL,
		ReadmeExcerpt:   readmeExcerpt,
	}
}
