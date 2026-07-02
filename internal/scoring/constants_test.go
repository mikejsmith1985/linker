package scoring

import "testing"

// TestConstantInvariants guards the product rules encoded as constants: the
// qualifying threshold and the strong-vs-soft gate relationship.
func TestConstantInvariants(t *testing.T) {
	if QualifyingScoreThreshold != 70 {
		t.Errorf("QualifyingScoreThreshold = %d, want 70", QualifyingScoreThreshold)
	}
	if EagerDocumentTopN != 3 {
		t.Errorf("EagerDocumentTopN = %d, want 3", EagerDocumentTopN)
	}
	// Strong gates must be able to sink a near-perfect fit below the threshold.
	if MaxScore-SalaryGatePenalty >= QualifyingScoreThreshold {
		t.Error("salary gate is not strong enough to gate a top fit")
	}
	if MaxScore-WorkLocationGatePenalty >= QualifyingScoreThreshold {
		t.Error("work-location gate is not strong enough to gate a top fit")
	}
	// Soft factors must not, on their own, gate a top fit.
	if MaxScore-TravelSoftPenalty < QualifyingScoreThreshold {
		t.Error("travel penalty is too strong to be soft")
	}
	if MaxScore-RelocateSoftPenalty < QualifyingScoreThreshold {
		t.Error("relocate penalty is too strong to be soft")
	}
}
