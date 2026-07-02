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
- Target roles: comma-separated list of 3 to 6 specific job titles this candidate is well qualified for based on their experience (e.g. "Scrum Master, Release Train Engineer, Agile Coach"). Use standard industry titles a job board would list.`

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
