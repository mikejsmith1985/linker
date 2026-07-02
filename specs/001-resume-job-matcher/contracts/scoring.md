# Contract: `internal/scoring`

Assigns each opening a 1–100 fit score with hybrid gating (FR-004, FR-005, FR-005a). Split into a deterministic gate (unit-testable, no LLM) and an LLM skill-fit scorer.

## Named constants (no magic numbers — Article IV)

```go
const (
    QualifyingScoreThreshold = 70  // FR-006: below this is dropped
    MaxScore                 = 100
    MinScore                 = 1
    EagerDocumentTopN        = 3   // FR-007: top 3 get eager document generation
    SalaryGatePenalty        = 45  // FR-005a: strong gate — normally pushes below threshold
    WorkLocationGatePenalty  = 40  // FR-005a: strong gate
    TravelSoftPenalty        = 8   // FR-005a: soft factor
    RelocateSoftPenalty      = 8   // FR-005a: soft factor
)
```

## Gate (deterministic)

```go
// ApplyGates returns the total penalty and which gates fired, given an opening
// and the user's preferences. Pure function; no I/O; <10ms.
func ApplyGates(opening JobOpening, prefs SearchPreferences) (penalty int, fired GatePenalties)
```

Rules:
- Salary gate fires when `prefs.required_salary_min > 0` AND the opening's `salary_max` (or `salary_min`) is below it. When the opening states no salary, the salary gate does NOT fire but the uncertainty is noted in the explanation (edge case).
- Work-location gate fires when the opening's `work_location_type` conflicts with `prefs.work_location_pref` (e.g., `onsite` opening vs `remote` preference; `hybrid` preference is satisfied by `hybrid` or `remote`).
- Travel/relocate soft penalties apply small deductions when the opening implies travel/relocation the user is unwilling to do; they never act as gates.

## Scorer (LLM skill-fit)

```go
// ScoreFit asks Claude to rate resume-to-opening skill fit on a bounded base
// scale and to return a short, evidence-citing explanation (FR-015).
func ScoreFit(ctx, profile StructuredProfile, opening JobOpening) (baseFit int, explanation string, err error)
```

## Composition

```
final = clamp(baseFit - gatePenalty, MinScore, MaxScore)
isQualifying = final >= QualifyingScoreThreshold
```

## Contract tests

- A remote-only preference + onsite opening yields `WorkLocationGatePenalty` and a final score `< 70` for a mid base fit (unit).
- A salary below `required_salary_min` fires `SalaryGatePenalty`; an opening with no stated salary does NOT (unit).
- Travel/relocate produce only soft deductions, never dropping a strong fit below 70 on their own (unit).
- Final score is always clamped to 1–100 (unit, property-style).
- Changing a preference changes the ranking of the same opening set (integration; SC-005).
