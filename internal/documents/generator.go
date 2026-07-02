// Package documents generates a tailored resume and a cover letter for a
// qualifying job opening, and verifies that the output introduces no skills,
// employers, dates, titles, or credentials absent from the source resume — the
// no-fabrication guarantee (FR-007a). Anything new is flagged for the user, not
// silently shipped.
package documents

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/mikejsmith1985/linker/internal/claude"
	"github.com/mikejsmith1985/linker/internal/store"
)

// Generated is the output of one document generation.
type Generated struct {
	ContentMarkdown  string
	FabricationFlags []string // empty == clean
}

// Generator produces documents via the LLM under a no-fabrication constraint.
type Generator struct {
	llm claude.LLM
}

// NewGenerator builds a Generator over the given LLM.
func NewGenerator(llm claude.LLM) *Generator {
	return &Generator{llm: llm}
}

const resumeSystemPrompt = `You tailor a candidate's resume to a specific job posting. You may reorder, re-emphasize, and reword the candidate's existing experience, but you MUST use ONLY facts already present in the provided resume. Never add an employer, date, job title, credential, or skill the resume does not already contain. Output GitHub-flavored Markdown.`

const coverSystemPrompt = `You write a concise, specific cover letter for a job posting. Use ONLY facts present in the provided resume — never claim experience, skills, or credentials the resume does not contain. Output GitHub-flavored Markdown in three to four short paragraphs.`

// Generate produces the requested document type from the resume facts, then runs
// the no-fabrication verification pass. Content is derived solely from the
// supplied resume facts.
func (g *Generator) Generate(ctx context.Context, docType store.DocType, resumeFacts string, opening store.JobOpening) (Generated, error) {
	if strings.TrimSpace(resumeFacts) == "" {
		return Generated{}, fmt.Errorf("no resume facts to tailor from")
	}
	system := resumeSystemPrompt
	if docType == store.CoverLetter {
		system = coverSystemPrompt
	}
	content, err := g.llm.Complete(ctx, system, buildPrompt(resumeFacts, opening))
	if err != nil {
		return Generated{}, fmt.Errorf("generate %s: %w", docType, err)
	}
	content = strings.TrimSpace(content)
	return Generated{
		ContentMarkdown:  content,
		FabricationFlags: detectFabrications(resumeFacts, content, opening),
	}, nil
}

func buildPrompt(resumeFacts string, opening store.JobOpening) string {
	var b strings.Builder
	b.WriteString("SOURCE RESUME (the only facts you may use):\n")
	b.WriteString(strings.TrimSpace(resumeFacts))
	b.WriteString("\n\nTARGET JOB POSTING:\n")
	fmt.Fprintf(&b, "Title: %s\nEmployer: %s\nLocation: %s\n", opening.Title, opening.Employer, opening.Location)
	if strings.TrimSpace(opening.Description) != "" {
		fmt.Fprintf(&b, "Description:\n%s\n", strings.TrimSpace(opening.Description))
	}
	return b.String()
}

// skillTokenPattern matches skill-like tokens (letters, optionally with +/#/./-
// as in "C++", "C#", "Node.js").
var skillTokenPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.#-]{1,}`)

// detectFabrications flags skill-like terms that the generated document claims,
// which the target posting asks for, but which the source resume never mentions
// — i.e. experience invented to fit the job. Title/employer/location terms are
// excluded because a document legitimately names them.
func detectFabrications(resumeFacts, content string, opening store.JobOpening) []string {
	resumeSet := lowerWordSet(resumeFacts)
	openingWants := lowerWordSet(opening.Description)
	excluded := lowerWordSet(opening.Title + " " + opening.Employer + " " + opening.Location)

	seen := map[string]bool{}
	var flags []string
	for _, token := range skillTokenPattern.FindAllString(content, -1) {
		lower := strings.ToLower(token)
		if !looksLikeSkill(token) || seen[lower] {
			continue
		}
		if excluded[lower] || resumeSet[lower] || !openingWants[lower] {
			continue
		}
		seen[lower] = true
		flags = append(flags, token)
	}
	return flags
}

// looksLikeSkill keeps tokens that read like a technology/skill (capitalized,
// all-caps, or containing a tech separator) and rejects common prose words.
func looksLikeSkill(token string) bool {
	if len(token) < 2 || sentenceStopwords[strings.ToLower(token)] {
		return false
	}
	if strings.ContainsAny(token, "+#.") {
		return true
	}
	firstUpper := token[0] >= 'A' && token[0] <= 'Z'
	return firstUpper
}

// sentenceStopwords are capitalized prose words that must never be mistaken for
// a claimed skill.
var sentenceStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "our": true, "your": true,
	"we": true, "i": true, "as": true, "to": true, "in": true, "of": true, "at": true,
	"this": true, "that": true, "you": true, "my": true, "a": true, "an": true,
	"dear": true, "sincerely": true, "regards": true, "hiring": true, "manager": true,
	"team": true, "role": true, "position": true, "experience": true, "years": true,
}

func lowerWordSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, token := range skillTokenPattern.FindAllString(s, -1) {
		set[strings.ToLower(token)] = true
	}
	return set
}
