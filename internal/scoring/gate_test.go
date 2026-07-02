package scoring

import (
	"testing"

	"github.com/mikejsmith1985/linker/internal/store"
)

func TestSalaryGateFiresBelowMinimum(t *testing.T) {
	opening := store.JobOpening{SalaryMin: 90000, SalaryMax: 110000, WorkLocationType: store.WorkRemote}
	prefs := store.Preferences{RequiredSalaryMin: 150000, WorkLocationPref: store.WorkRemote}

	got := ApplyGates(opening, prefs)
	if got.Fired[GateSalary] != SalaryGatePenalty {
		t.Errorf("salary gate = %d, want %d", got.Fired[GateSalary], SalaryGatePenalty)
	}
}

func TestSalaryGateDoesNotFireWhenSalaryUnstated(t *testing.T) {
	opening := store.JobOpening{WorkLocationType: store.WorkRemote} // no salary
	prefs := store.Preferences{RequiredSalaryMin: 150000, WorkLocationPref: store.WorkRemote}

	got := ApplyGates(opening, prefs)
	if _, fired := got.Fired[GateSalary]; fired {
		t.Error("salary gate fired despite no stated salary")
	}
	if len(got.Notes) == 0 {
		t.Error("expected a disclosure note about missing salary")
	}
}

func TestWorkLocationGateFiresForOnsiteWhenRemoteWanted(t *testing.T) {
	opening := store.JobOpening{WorkLocationType: store.WorkOnsite, Location: "NYC"}
	prefs := store.Preferences{WorkLocationPref: store.WorkRemote, WillingToRelocate: true}

	got := ApplyGates(opening, prefs)
	if got.Fired[GateWorkLocation] != WorkLocationGatePenalty {
		t.Errorf("work-location gate = %d, want %d", got.Fired[GateWorkLocation], WorkLocationGatePenalty)
	}
}

func TestWorkLocationGateAcceptsRemoteForHybridPref(t *testing.T) {
	opening := store.JobOpening{WorkLocationType: store.WorkRemote}
	prefs := store.Preferences{WorkLocationPref: store.WorkHybrid}

	got := ApplyGates(opening, prefs)
	if _, fired := got.Fired[GateWorkLocation]; fired {
		t.Error("hybrid preference should accept a remote opening")
	}
}

func TestUnknownWorkLocationNeverGates(t *testing.T) {
	opening := store.JobOpening{WorkLocationType: store.WorkUnknown}
	prefs := store.Preferences{WorkLocationPref: store.WorkRemote}

	got := ApplyGates(opening, prefs)
	if _, fired := got.Fired[GateWorkLocation]; fired {
		t.Error("unknown work-location should not gate")
	}
}

func TestTravelIsSoftNotAGate(t *testing.T) {
	opening := store.JobOpening{
		WorkLocationType: store.WorkRemote,
		Description:      "Requires up to 25% travel to client sites.",
	}
	prefs := store.Preferences{WillingToTravel: false, WorkLocationPref: store.WorkRemote}

	got := ApplyGates(opening, prefs)
	if got.Fired[GateTravel] != TravelSoftPenalty {
		t.Errorf("travel penalty = %d, want %d", got.Fired[GateTravel], TravelSoftPenalty)
	}
	// A soft penalty alone must not push a strong fit below the threshold.
	if TravelSoftPenalty >= QualifyingScoreThreshold {
		t.Error("travel soft penalty is too large to be 'soft'")
	}
}

func TestStrongGatesPushBelowThreshold(t *testing.T) {
	// A strong base fit gated on salary+work-location should end up below 70.
	opening := store.JobOpening{SalaryMin: 50000, SalaryMax: 60000, WorkLocationType: store.WorkOnsite, Location: "NYC"}
	prefs := store.Preferences{RequiredSalaryMin: 150000, WorkLocationPref: store.WorkRemote, WillingToRelocate: true}

	got := ApplyGates(opening, prefs)
	const strongBaseFit = 95
	final := strongBaseFit - got.Penalty
	if final >= QualifyingScoreThreshold {
		t.Errorf("gated final = %d, want < %d", final, QualifyingScoreThreshold)
	}
}
