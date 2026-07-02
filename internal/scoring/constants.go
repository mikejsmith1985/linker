// Package scoring rates a job opening against a resume on a 1–100 scale using a
// deterministic preference gate (salary + work-location act as strong gates;
// travel/relocate are soft factors) blended with an LLM skill-fit assessment.
package scoring

const (
	// QualifyingScoreThreshold is the minimum score an opening must reach to be
	// shown to the user and to receive tailored documents.
	QualifyingScoreThreshold = 70

	// MaxScore and MinScore bound the final score.
	MaxScore = 100
	MinScore = 1

	// EagerDocumentTopN is how many of the highest-scoring qualifying openings
	// get their tailored documents generated eagerly during the search.
	EagerDocumentTopN = 3

	// Strong gates: a mismatch normally drives the score below the threshold.
	SalaryGatePenalty       = 45
	WorkLocationGatePenalty = 40

	// Soft factors: they nudge the score without acting as gates.
	TravelSoftPenalty   = 8
	RelocateSoftPenalty = 8
)

// Gate penalty keys recorded on a match result.
const (
	GateSalary       = "salary"
	GateWorkLocation = "work_location"
	GateTravel       = "travel"
	GateRelocate     = "relocate"
)
