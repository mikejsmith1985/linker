# Contract: `internal/documents`

Generates a tailored resume and cover letter per qualifying opening, under the no-fabrication rule (FR-007, FR-007a, FR-008, FR-010).

## Interface

```go
// Generate produces a tailored document for a qualifying match. It MUST derive
// content solely from resumeFacts (the deterministically extracted resume text)
// and MUST return any entities/skills it introduced that are absent from
// resumeFacts as fabricationFlags rather than embedding them silently.
func Generate(ctx, docType DocType, resumeFacts string, opening JobOpening) (Generated, error)

type DocType string
const (
    TailoredResume DocType = "tailored_resume"
    CoverLetter    DocType = "cover_letter"
)

type Generated struct {
    ContentMarkdown  string
    FabricationFlags []string // empty == clean; non-empty surfaces to user for review
}
```

## Rules

- MUST NOT be called for a match with `score < 70` (FR-008). Caller enforces; generator also guards.
- Eager for `rank` 1–3 during the search; on-demand + cached for the rest (FR-007). Caching = a `GeneratedDocument` row exists.
- No-fabrication: a post-generation verification pass extracts named entities/skills from the output and diffs against `resumeFacts`; anything new lands in `FabricationFlags` (FR-007a). The verification is deterministic enough to unit-test with fixtures.
- Output is Markdown; downstream renders/edits it and offers text/PDF download (FR-010).

## Contract tests

- Generating for a `score<70` match returns an error / is refused (unit).
- Given resume facts lacking "Kubernetes" and an opening demanding it, if the output claims Kubernetes experience, it appears in `FabricationFlags` (unit with fixture).
- A clean tailored resume (only reordered/reworded facts) returns empty `FabricationFlags` (unit with fixture).
- Second open of the same match returns the cached document, not a regenerated one (integration).
