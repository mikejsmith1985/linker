package resume

import (
	"context"
	"fmt"
	"strings"

	"github.com/mikejsmith1985/linker/internal/claude"
)

const profileSystemPrompt = `You organize a resume's raw text into a concise structured profile used for job matching. Extract ONLY what is present in the text — never invent skills, employers, dates, titles, or credentials.

Produce plain text with these sections:
- Skills: comma-separated technical and professional skills actually mentioned.
- Experience: bullet lines of role @ employer (dates) — one per position found.
- Credentials: degrees/certifications mentioned, or "none stated".
- Target roles: comma-separated list of 4 to 8 specific job titles a job board would list. Include both roles the candidate is well qualified for today AND 1-3 adjacent roles they could realistically grow into. When the resume shows any AI, automation, or agentic-tooling experience, include emerging AI-first titles (e.g. "AI Delivery Lead", "AI Program Manager", "Agentic AI Engineer", "AI Transformation Lead", "AI Workflow Engineer"). Use standard, searchable industry titles.`

// ExtractProfile asks the LLM to organize raw resume text into a structured
// profile. The profile is derived solely from the supplied text (no fabrication).
func ExtractProfile(ctx context.Context, llm claude.LLM, rawText string) (string, error) {
	if strings.TrimSpace(rawText) == "" {
		return "", ErrUnreadable
	}
	profile, err := llm.Complete(ctx, profileSystemPrompt, "RESUME TEXT:\n"+strings.TrimSpace(rawText))
	if err != nil {
		return "", fmt.Errorf("extract profile: %w", err)
	}
	if strings.TrimSpace(profile) == "" {
		return "", fmt.Errorf("profile extraction returned empty result")
	}
	return strings.TrimSpace(profile), nil
}
