// Package assistant is the in-app conversational helper. It lets the user refine
// their job search in natural language: it reads the current preferences and
// latest matches, answers questions, and — when asked — updates preferences or
// starts a new search. It plans actions as a single structured JSON response so
// the logic stays testable with a fake LLM.
package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mikejsmith1985/linker/internal/claude"
	"github.com/mikejsmith1985/linker/internal/store"
)

// Store is the slice of persistence the assistant reads and writes.
type Store interface {
	GetPreferences(ctx context.Context) (store.Preferences, error)
	SavePreferences(ctx context.Context, p store.Preferences) (int64, error)
	LatestCompletedSearchID(ctx context.Context) (int64, error)
	ListQualifying(ctx context.Context, searchID int64) ([]store.MatchWithOpening, error)
	AppendChatMessage(ctx context.Context, role, content string) error
	ListChatMessages(ctx context.Context, limit int) ([]store.ChatMessage, error)
}

// Searcher starts a background search.
type Searcher interface {
	StartSearch(ctx context.Context) (int64, error)
}

// Assistant answers a user message and acts on the app on their behalf.
type Assistant struct {
	llm      claude.LLM
	store    Store
	searcher Searcher
}

// New builds the assistant.
func New(llm claude.LLM, st Store, searcher Searcher) *Assistant {
	return &Assistant{llm: llm, store: st, searcher: searcher}
}

const systemPrompt = `You are the assistant inside "linker", a resume-driven job matcher. Help the user refine their job search through conversation. You can read their current preferences and latest matches (given below) and, when they ask, change preferences or start a new search.

Respond with ONLY a single JSON object and nothing else:
{
  "reply": "<your natural-language message to the user>",
  "update_preferences": null,
  "start_search": false
}

To change settings, replace update_preferences with an object containing ONLY the fields to change:
- "work_location": "remote" | "hybrid" | "onsite"
- "strict_work_location": true|false   (when true + remote, hybrid/onsite roles are excluded entirely)
- "required_salary_min": integer annual salary, 0 to unset
- "location": string, e.g. "United States"
- "new_roles_only": true|false   (exclude postings already seen in a prior search)
- "add_target_roles": ["Job Title", ...]  (extra role titles to search for)

Set "start_search": true only when the user asks to search or it is clearly implied by their request. Answer questions about their preferences and matches directly in "reply" using the data provided. Be concise, friendly, and concrete.`

// plan is the model's structured response.
type plan struct {
	Reply             string      `json:"reply"`
	UpdatePreferences *prefUpdate `json:"update_preferences"`
	StartSearch       bool        `json:"start_search"`
}

// prefUpdate carries only the preference fields the model wants to change.
type prefUpdate struct {
	WorkLocation       *string  `json:"work_location"`
	StrictWorkLocation *bool    `json:"strict_work_location"`
	RequiredSalaryMin  *int     `json:"required_salary_min"`
	Location           *string  `json:"location"`
	NewRolesOnly       *bool    `json:"new_roles_only"`
	AddTargetRoles     []string `json:"add_target_roles"`
}

// Handle answers a user message, applying any requested actions, and returns the
// assistant's reply. The conversation is persisted.
func (a *Assistant) Handle(ctx context.Context, userText string) (string, error) {
	prefs, err := a.store.GetPreferences(ctx)
	if err != nil {
		return "", fmt.Errorf("load preferences: %w", err)
	}
	history, _ := a.store.ListChatMessages(ctx, 20)

	prompt := a.buildPrompt(ctx, prefs, history, userText)
	raw, err := a.llm.Complete(ctx, systemPrompt, prompt)
	if err != nil {
		return "", fmt.Errorf("assistant llm: %w", err)
	}

	parsed := parsePlan(raw)
	notes := a.applyActions(ctx, &prefs, parsed)

	reply := strings.TrimSpace(parsed.Reply)
	if reply == "" {
		reply = "Done."
	}
	if len(notes) > 0 {
		reply += "\n\n— " + strings.Join(notes, "; ") + "."
	}

	_ = a.store.AppendChatMessage(ctx, "user", userText)
	_ = a.store.AppendChatMessage(ctx, "assistant", reply)
	return reply, nil
}

// applyActions performs the plan's preference update and/or search, returning
// human-readable notes about what happened.
func (a *Assistant) applyActions(ctx context.Context, prefs *store.Preferences, p plan) []string {
	var notes []string
	if p.UpdatePreferences != nil {
		applyPrefUpdate(prefs, p.UpdatePreferences)
		if _, err := a.store.SavePreferences(ctx, *prefs); err == nil {
			notes = append(notes, "updated your preferences")
		}
	}
	if p.StartSearch {
		if _, err := a.searcher.StartSearch(ctx); err == nil {
			notes = append(notes, "started a new search (results will appear on Matches)")
		} else {
			notes = append(notes, "couldn't start a search: "+err.Error())
		}
	}
	return notes
}

func applyPrefUpdate(prefs *store.Preferences, u *prefUpdate) {
	if u.WorkLocation != nil {
		prefs.WorkLocationPref = store.WorkLocation(strings.ToLower(*u.WorkLocation))
	}
	if u.StrictWorkLocation != nil {
		prefs.StrictWorkLocation = *u.StrictWorkLocation
	}
	if u.RequiredSalaryMin != nil {
		prefs.RequiredSalaryMin = *u.RequiredSalaryMin
	}
	if u.Location != nil {
		prefs.Location = strings.TrimSpace(*u.Location)
	}
	if u.NewRolesOnly != nil {
		prefs.NewRolesOnly = *u.NewRolesOnly
	}
	for _, role := range u.AddTargetRoles {
		if role = strings.TrimSpace(role); role != "" && !containsFold(prefs.TargetRoles, role) {
			prefs.TargetRoles = append(prefs.TargetRoles, role)
		}
	}
}

// buildPrompt assembles the state (preferences + latest matches + recent chat)
// plus the new user message for the model.
func (a *Assistant) buildPrompt(ctx context.Context, prefs store.Preferences, history []store.ChatMessage, userText string) string {
	var b strings.Builder
	b.WriteString("CURRENT PREFERENCES:\n")
	b.WriteString(describePreferences(prefs))
	b.WriteString("\n\nLATEST MATCHES:\n")
	b.WriteString(a.describeMatches(ctx))
	if len(history) > 0 {
		b.WriteString("\n\nRECENT CONVERSATION:\n")
		for _, m := range history {
			fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Content)
		}
	}
	b.WriteString("\nUSER MESSAGE:\n")
	b.WriteString(userText)
	return b.String()
}

func describePreferences(p store.Preferences) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- Work location: %s (strict=%v)\n", p.WorkLocationPref, p.StrictWorkLocation)
	fmt.Fprintf(&b, "- Required minimum salary: %d\n", p.RequiredSalaryMin)
	fmt.Fprintf(&b, "- Location/region: %s\n", p.Location)
	fmt.Fprintf(&b, "- New roles only: %v\n", p.NewRolesOnly)
	fmt.Fprintf(&b, "- Willing to travel: %v; relocate: %v\n", p.WillingToTravel, p.WillingToRelocate)
	if len(p.TargetRoles) > 0 {
		fmt.Fprintf(&b, "- Target roles: %s\n", strings.Join(p.TargetRoles, ", "))
	}
	return b.String()
}

func (a *Assistant) describeMatches(ctx context.Context) string {
	id, err := a.store.LatestCompletedSearchID(ctx)
	if err != nil {
		return "(no completed searches yet)"
	}
	matches, err := a.store.ListQualifying(ctx, id)
	if err != nil || len(matches) == 0 {
		return "(no qualifying matches in the latest search)"
	}
	var b strings.Builder
	for i, m := range matches {
		if i >= 15 {
			fmt.Fprintf(&b, "…and %d more.\n", len(matches)-15)
			break
		}
		status := m.Opening.ReviewStatus
		fmt.Fprintf(&b, "%d. %s @ %s — score %d [%s] (%s)\n",
			i+1, m.Opening.Title, m.Opening.Employer, m.Score, status, strings.TrimSpace(firstSentence(m.ScoreExplanation)))
	}
	return b.String()
}

func firstSentence(s string) string {
	if i := strings.IndexAny(s, ".\n"); i > 0 {
		return s[:i]
	}
	return s
}

// parsePlan extracts the JSON plan, tolerating surrounding prose. When no JSON is
// present the whole text becomes the reply.
func parsePlan(raw string) plan {
	obj := extractJSONObject(raw)
	if obj != "" {
		var p plan
		if err := json.Unmarshal([]byte(obj), &p); err == nil {
			return p
		}
	}
	return plan{Reply: strings.TrimSpace(raw)}
}

func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return ""
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}
