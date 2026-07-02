package scoring

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mikejsmith1985/linker/internal/claude"
	"github.com/mikejsmith1985/linker/internal/store"
)

// Score is the final assessment of one opening against the resume.
type Score struct {
	Value         int
	Explanation   string
	GatePenalties map[string]int
	IsQualifying  bool
}

// Scorer blends an LLM skill-fit judgment with the deterministic preference gate.
type Scorer struct {
	llm claude.LLM
}

// NewScorer builds a Scorer over the given LLM.
func NewScorer(llm claude.LLM) *Scorer {
	return &Scorer{llm: llm}
}

const scorerSystemPrompt = `You are a precise job-fit evaluator. Given a candidate's resume profile and a job posting, rate ONLY the skills-and-experience fit — ignore salary, location, and logistics, which are handled separately.

Respond with a single JSON object and nothing else:
{"fit": <integer 1-100>, "explanation": "<one or two sentences citing concrete evidence from the resume and posting>"}`

// Score rates one opening. The LLM returns a pure skill-fit base (1–100); the
// deterministic gate penalties are then subtracted and the result clamped.
func (s *Scorer) Score(ctx context.Context, profile string, opening store.JobOpening, prefs store.Preferences) (Score, error) {
	baseFit, explanation, err := s.skillFit(ctx, profile, opening)
	if err != nil {
		return Score{}, err
	}

	gate := ApplyGates(opening, prefs)
	final := clamp(baseFit-gate.Penalty, MinScore, MaxScore)

	return Score{
		Value:         final,
		Explanation:   composeExplanation(explanation, gate),
		GatePenalties: gate.Fired,
		IsQualifying:  final >= QualifyingScoreThreshold,
	}, nil
}

func (s *Scorer) skillFit(ctx context.Context, profile string, opening store.JobOpening) (int, string, error) {
	prompt := buildFitPrompt(profile, opening)
	raw, err := s.llm.Complete(ctx, scorerSystemPrompt, prompt)
	if err != nil {
		return 0, "", fmt.Errorf("skill-fit: %w", err)
	}
	fit, explanation := parseFit(raw)
	return fit, explanation, nil
}

func buildFitPrompt(profile string, opening store.JobOpening) string {
	var b strings.Builder
	b.WriteString("CANDIDATE RESUME PROFILE:\n")
	b.WriteString(strings.TrimSpace(profile))
	b.WriteString("\n\nJOB POSTING:\n")
	fmt.Fprintf(&b, "Title: %s\nEmployer: %s\nLocation: %s\n", opening.Title, opening.Employer, opening.Location)
	if strings.TrimSpace(opening.Description) != "" {
		fmt.Fprintf(&b, "Description:\n%s\n", strings.TrimSpace(opening.Description))
	}
	return b.String()
}

// fitResponse is the model's structured reply.
type fitResponse struct {
	Fit         int    `json:"fit"`
	Explanation string `json:"explanation"`
}

var intPattern = regexp.MustCompile(`\d{1,3}`)

// parseFit extracts the fit score and explanation, tolerating extra prose around
// the JSON. It clamps the fit to the valid range.
func parseFit(raw string) (int, string) {
	if obj := extractJSONObject(raw); obj != "" {
		var parsed fitResponse
		if err := json.Unmarshal([]byte(obj), &parsed); err == nil && parsed.Fit > 0 {
			return clamp(parsed.Fit, MinScore, MaxScore), strings.TrimSpace(parsed.Explanation)
		}
	}
	// Fallback: first integer in the text, whole text as explanation.
	if m := intPattern.FindString(raw); m != "" {
		if n, err := strconv.Atoi(m); err == nil {
			return clamp(n, MinScore, MaxScore), strings.TrimSpace(raw)
		}
	}
	return MinScore, strings.TrimSpace(raw)
}

func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return ""
}

func composeExplanation(fit string, gate GateResult) string {
	parts := []string{}
	if strings.TrimSpace(fit) != "" {
		parts = append(parts, fit)
	}
	for _, note := range gate.Notes {
		parts = append(parts, note)
	}
	if len(gate.Fired) > 0 {
		parts = append(parts, "Preference gates applied: "+describeGates(gate.Fired)+".")
	}
	return strings.Join(parts, " ")
}

func describeGates(fired map[string]int) string {
	labels := []string{}
	for _, key := range []string{GateSalary, GateWorkLocation, GateTravel, GateRelocate} {
		if _, ok := fired[key]; ok {
			labels = append(labels, key)
		}
	}
	return strings.Join(labels, ", ")
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
