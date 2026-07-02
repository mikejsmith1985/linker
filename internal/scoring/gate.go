package scoring

import (
	"strings"

	"github.com/mikejsmith1985/linker/internal/store"
)

// GateResult is the deterministic preference assessment of one opening.
type GateResult struct {
	// Penalty is the total to subtract from the LLM base fit.
	Penalty int
	// Fired maps each gate that applied to the penalty it contributed.
	Fired map[string]int
	// Notes explains disclosures that are not penalties (e.g. no salary stated).
	Notes []string
}

// ApplyGates evaluates an opening against the user's preferences. It is a pure
// function (no I/O) so it is unit-testable in well under 10ms. Required salary
// and work-location act as strong gates; willingness to travel and relocate act
// as soft factors.
func ApplyGates(opening store.JobOpening, prefs store.Preferences) GateResult {
	result := GateResult{Fired: map[string]int{}}

	applySalaryGate(&result, opening, prefs)
	applyWorkLocationGate(&result, opening, prefs)
	applyLocationGate(&result, opening, prefs)
	applyTravelPenalty(&result, opening, prefs)
	applyRelocatePenalty(&result, opening, prefs)

	return result
}

// applyLocationGate fires when the opening states a geographic eligibility that
// excludes the user's location. It is "innocent until proven guilty": it only
// gates when the posting names a recognized foreign-only region and offers no
// US-inclusive or worldwide option, so plain city names never over-gate.
func applyLocationGate(r *GateResult, opening store.JobOpening, prefs store.Preferences) {
	if strings.TrimSpace(prefs.Location) == "" || strings.TrimSpace(opening.Location) == "" {
		return
	}
	if !locationEligible(prefs.Location, opening.Location) {
		r.Penalty += LocationGatePenalty
		r.Fired[GateLocation] = LocationGatePenalty
	}
}

// worldwideTokens indicate a posting open to candidates anywhere.
var worldwideTokens = []string{"worldwide", "anywhere", "global", "fully remote", "any location", "remote - global"}

// usTokens indicate a posting is open to US-based candidates.
var usTokens = []string{"united states", "usa", "u.s.a", "u.s.", "us-based", "us based", "us only", "us remote", "us timezone", "north america", "northern america", "americas"}

// foreignOnlyTokens are region names that, absent any US/worldwide token, imply a
// posting is not open to US candidates.
var foreignOnlyTokens = []string{
	"brazil", "europe", "united kingdom", "uk", "germany", "france", "spain", "portugal",
	"netherlands", "poland", "ukraine", "india", "latam", "latin america", "central america",
	"south america", "apac", "emea", "africa", "asia", "oceania", "australia", "canada",
	"israel", "mexico", "argentina", "philippines", "singapore", "japan",
}

// locationEligible reports whether a US-based user can take a posting given its
// stated required location.
func locationEligible(userLocation, jobLocation string) bool {
	job := strings.ToLower(jobLocation)
	if containsAny(job, worldwideTokens) {
		return true
	}
	if userIsUSBased(userLocation) {
		if containsAny(job, usTokens) {
			return true
		}
		// A recognized foreign-only restriction with no US option → ineligible.
		return !containsAny(job, foreignOnlyTokens)
	}
	// Non-US users: eligible if the posting mentions their location or is unrecognized.
	return strings.Contains(job, strings.ToLower(strings.TrimSpace(userLocation))) || !containsAny(job, foreignOnlyTokens)
}

func userIsUSBased(userLocation string) bool {
	u := strings.ToLower(userLocation)
	return strings.Contains(u, "united states") || strings.Contains(u, "usa") || u == "us" || strings.Contains(u, "u.s")
}

func containsAny(haystack string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(haystack, token) {
			return true
		}
	}
	return false
}

// applySalaryGate fires when the opening states a salary below the required
// minimum. When the opening states no salary, the gate does not fire but the
// uncertainty is disclosed.
func applySalaryGate(r *GateResult, opening store.JobOpening, prefs store.Preferences) {
	if prefs.RequiredSalaryMin <= 0 {
		return
	}
	best := opening.SalaryMax
	if best == 0 {
		best = opening.SalaryMin
	}
	if best == 0 {
		r.Notes = append(r.Notes, "posting states no salary; salary gate not applied")
		return
	}
	if best < prefs.RequiredSalaryMin {
		r.Penalty += SalaryGatePenalty
		r.Fired[GateSalary] = SalaryGatePenalty
	}
}

// applyWorkLocationGate fires when the opening's work-location type conflicts
// with the user's preference. An unknown opening type never gates.
func applyWorkLocationGate(r *GateResult, opening store.JobOpening, prefs store.Preferences) {
	if opening.WorkLocationType == store.WorkUnknown || prefs.WorkLocationPref == "" {
		return
	}
	if workLocationConflicts(prefs.WorkLocationPref, opening.WorkLocationType) {
		r.Penalty += WorkLocationGatePenalty
		r.Fired[GateWorkLocation] = WorkLocationGatePenalty
	}
}

// workLocationConflicts encodes what each preference will accept:
//   - remote: only remote openings satisfy it
//   - hybrid: hybrid or remote satisfy it
//   - onsite: onsite or hybrid satisfy it
func workLocationConflicts(pref, opening store.WorkLocation) bool {
	switch pref {
	case store.WorkRemote:
		return opening != store.WorkRemote
	case store.WorkHybrid:
		return opening == store.WorkOnsite
	case store.WorkOnsite:
		return opening == store.WorkRemote
	default:
		return false
	}
}

// applyTravelPenalty is a soft deduction when the posting implies travel and the
// user is unwilling to travel.
func applyTravelPenalty(r *GateResult, opening store.JobOpening, prefs store.Preferences) {
	if prefs.WillingToTravel {
		return
	}
	if strings.Contains(strings.ToLower(opening.Description), "travel") {
		r.Penalty += TravelSoftPenalty
		r.Fired[GateTravel] = TravelSoftPenalty
	}
}

// applyRelocatePenalty is a soft deduction when a physical-presence opening in a
// specific location would require relocation the user is unwilling to make.
func applyRelocatePenalty(r *GateResult, opening store.JobOpening, prefs store.Preferences) {
	if prefs.WillingToRelocate {
		return
	}
	requiresPresence := opening.WorkLocationType == store.WorkOnsite || opening.WorkLocationType == store.WorkHybrid
	if requiresPresence && strings.TrimSpace(opening.Location) != "" {
		r.Penalty += RelocateSoftPenalty
		r.Fired[GateRelocate] = RelocateSoftPenalty
	}
}
