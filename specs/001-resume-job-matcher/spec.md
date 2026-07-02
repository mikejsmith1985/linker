# Feature Specification: Resume-Driven Job Matcher

**Feature Directory**: `specs/001-resume-job-matcher`
**Created**: 2026-07-02
**Status**: Ready for planning
**Input**: "Create an application that searches LinkedIn and other job boards for jobs specific to a provided resume. Matching jobs are identified, scored 1–100, and pulled into a single view. Jobs below 70 are ignored. Jobs scoring 70+ get a generated cover letter and tailored resume. The user reviews each job with its score, clicks the ones to proceed with, the posting opens, and the user submits the tailored resume and cover letter manually. Additional scoring inputs: willingness to travel, willingness to relocate, required salary, and working condition (onsite / hybrid / remote)."

---

## Clarifications

### Session 2026-07-02

- Q: Are the preference inputs hard filters or soft scoring factors? → A: Hybrid — required salary and work-location act as strong gates (a mismatch applies a large penalty that normally drives the score below 70), while willingness to travel and willingness to relocate are softer scoring factors.
- Q: What is the truthfulness guardrail for the generated tailored resume? → A: No fabrication — the tailored resume may only reorder, re-emphasize, and reword facts already present in the source resume; it must never add employers, dates, titles, credentials, or skills the user did not state.
- Q: Do search results and generated documents persist between runs, or are they ephemeral? → A: Persist — searches, scored openings, generated documents, and selections are saved and reloadable across restarts.
- Q: Does the app hold one resume at a time or a library of resumes? → A: Single active resume — one at a time; uploading a new resume replaces the current one.
- Q: Are tailored documents generated eagerly for all 70+ jobs or on demand? → A: Eager for the top 3 highest-scoring openings; all other qualifying openings generate their documents on first open and cache thereafter.

## User Scenarios & Testing *(mandatory)*

### Primary User Story

A job seeker uploads their resume and states their preferences (salary floor, work-location preference, and willingness to travel or relocate). The application searches configured job boards for openings that fit the resume's skills and the stated preferences. Each opening is scored from 1 to 100 for fit. Openings scoring below 70 are discarded. For every opening scoring 70 or above, the application generates a tailored version of the resume and a cover letter written for that specific posting. The job seeker opens a single dashboard, sees every qualifying opening with its score and generated documents, selects the ones worth pursuing, and for each selection the original posting opens so they can submit the tailored documents themselves. The application never submits an application on the user's behalf.

### Acceptance Scenarios

1. **Given** a valid resume and preferences are provided, **When** the user starts a search, **Then** the application returns a ranked list of openings, each with a fit score from 1 to 100.
2. **Given** a search has completed, **When** the results are displayed, **Then** no opening with a score below 70 appears in the list.
3. **Given** an opening scored 70 or higher, **When** the user views it, **Then** a tailored resume and a cover letter specific to that opening are available for review and download (pre-generated for the top three scores; generated on first open otherwise).
4. **Given** an opening scored between 1 and 69, **When** results are prepared, **Then** the application does not generate a tailored resume or cover letter for it.
5. **Given** the user has stated a required salary, work-location preference, and travel/relocation willingness, **When** openings are scored, **Then** those preferences measurably affect each score (e.g., an onsite-only role for a remote-only user scores lower).
6. **Given** the user selects one or more qualifying openings, **When** they choose to proceed, **Then** each selected posting opens at its original source for manual submission.
7. **Given** a generated tailored resume or cover letter, **When** the user reviews it, **Then** they can edit it before downloading or submitting.
8. **Given** the same opening appears on more than one job board, **When** results are compiled, **Then** it appears only once in the single view.

### Edge Cases

- Resume is unreadable, empty, or in an unsupported format → the user is told what is wrong and how to fix it; no search runs on garbage input.
- A job board returns no openings, is unreachable, or rate-limits the request → the user sees which sources succeeded and which did not, and results from working sources are still shown.
- No opening reaches a score of 70 → the user sees a clear "no qualifying matches" state with the option to broaden preferences, not an empty screen.
- A posting lacks a stated salary → scoring proceeds using the available signals and the salary uncertainty is disclosed on the result.
- A posting's original URL is dead when the user clicks to proceed → the user is told the posting is no longer reachable.
- The user enables automated-browsing discovery without acknowledging the risk warning → the mode does not run until the acknowledgment is given.
- A pasted URL points to a page that is not a job posting, or cannot be fetched → the user is told that specific URL could not be assessed, and other pasted URLs still process.
- The resume lists skills that match many low-relevance postings → scoring must distinguish keyword overlap from genuine fit so the list is not flooded with weak matches.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST accept a resume from the user and extract the skills, experience, and qualifications used for matching.
- **FR-001a**: The system MUST maintain a single active resume; uploading a new resume replaces the current one. Prior searches and their results remain associated with the resume version that produced them.
- **FR-002**: The system MUST accept and store user preferences: required (minimum) salary, work-location preference (onsite / hybrid / remote), willingness to travel, and willingness to relocate.
- **FR-003**: The system MUST discover openings relevant to the resume primarily through compliant job-search APIs and job aggregators (sources that permit programmatic access under their terms).
- **FR-004**: The system MUST assign each discovered opening a fit score on an integer scale of 1 to 100.
- **FR-005**: The scoring MUST incorporate resume-to-opening skill fit AND the user's stated preferences (salary, work location, travel, relocation) as distinct contributing factors.
- **FR-005a**: Required salary and work-location preference MUST act as strong gates: an opening that misses the stated minimum salary or conflicts with the stated work-location preference MUST receive a large score penalty sufficient to normally place it below the 70 threshold. Willingness to travel and willingness to relocate MUST be treated as softer factors that adjust the score without acting as hard gates.
- **FR-006**: The system MUST exclude every opening scoring below 70 from the results presented to the user.
- **FR-007**: For every opening scoring 70 or above, the system MUST make a tailored resume and cover letter available. The three highest-scoring qualifying openings MUST have their documents generated automatically as part of the search; every other qualifying opening MUST have its documents generated on first open and cached so subsequent views reuse them.
- **FR-007a**: The tailored resume MUST be derived solely from facts present in the user's source resume — reordering, re-emphasizing, and rewording are allowed, but the system MUST NOT add employers, dates, job titles, credentials, or skills the user did not state. The cover letter is subject to the same no-fabrication rule.
- **FR-008**: The system MUST NOT generate tailored documents for openings scoring below 70.
- **FR-009**: The system MUST present all qualifying openings in a single consolidated view showing, at minimum, the job title, employer, score, and key match reasons.
- **FR-010**: The system MUST let the user review, edit, and download each generated tailored resume and cover letter before use.
- **FR-011**: The system MUST let the user select the openings they wish to pursue.
- **FR-012**: For each selected opening, the system MUST open the original job posting so the user can submit their application manually.
- **FR-013**: The system MUST NOT submit any application, resume, or cover letter to any employer or job board automatically; submission is always a manual user action.
- **FR-014**: The system MUST de-duplicate the same opening appearing across multiple job boards so it is presented once.
- **FR-015**: The system MUST show, for each opening, a human-readable explanation of why it scored as it did, so the score is trustworthy and reviewable.
- **FR-016**: The system MUST surface the health of each job-board source per search (succeeded / failed / no results) rather than silently dropping a source.
- **FR-017**: The system MUST allow a search to be re-run so the user can refresh results after updating their resume or preferences.
- **FR-018**: The system MUST validate resume input and reject unreadable or empty resumes with an actionable message. Accepted formats are PDF, DOCX, and plain text.
- **FR-019**: The system MUST operate as a single-user, self-hosted tool for one resume owner; it does not require user accounts or multi-tenant data isolation.
- **FR-020**: The system MUST run searches on demand (user-initiated) in the initial scope, and MUST be structured so that scheduled/continuous discovery can be added later without reworking the matching and scoring core.
- **FR-021**: The system MUST allow the user to paste one or more individual job-posting URLs and have each fetched, scored, and (if it reaches 70) given tailored documents — the same treatment as a discovered opening.
- **FR-022**: The system MUST support an optional automated-browsing discovery mode for sources that do not offer a permitted API (including LinkedIn), and MUST require the user to explicitly acknowledge the associated terms-of-service and account-risk warning before this mode runs.
- **FR-023**: The system MUST keep automated-browsing discovery (FR-022) disabled by default, so that no automated access to a non-API source occurs until the user opts in and acknowledges the risk.
- **FR-024**: The system MUST persist searches, discovered openings, their scores and score explanations, generated tailored documents, and the user's selections, so that all results remain available and reloadable after a restart.
- **FR-025**: On re-running a search, the system SHOULD reuse previously scored openings for the same posting rather than re-scoring identical postings, while still surfacing newly discovered openings.

### Key Entities

- **Resume**: The user's source resume; carries extracted skills, experience, and qualifications used as the matching baseline.
- **Search Preferences**: Required salary, work-location preference (onsite/hybrid/remote), willingness to travel, willingness to relocate. Inputs to scoring.
- **Job Opening**: A discovered posting — title, employer, location, work-location type, stated salary (if any), description, source board, and original URL.
- **Match Result**: The pairing of one Job Opening with the user's Resume and Preferences — carries the 1–100 score, the score explanation, and (for 70+) references to the tailored documents.
- **Tailored Resume**: A resume rewritten for one specific opening; editable and downloadable.
- **Cover Letter**: A cover letter written for one specific opening; editable and downloadable.
- **Selection**: The user's decision to pursue a given opening, and the record that its posting was opened for manual submission.

## Success Criteria *(mandatory)*

- **SC-001**: For a given resume and preferences, at least 90% of openings presented as qualifying are judged by the user as genuinely relevant to their background (score is trustworthy, not keyword noise).
- **SC-002**: 100% of openings shown to the user score 70 or above; no sub-70 opening is ever presented.
- **SC-003**: The three highest-scoring openings have a tailored resume and cover letter ready the moment results appear; every other qualifying opening produces its documents within a few seconds of first being opened. No qualifying opening is ever left without a path to its documents.
- **SC-004**: A user can go from "start search" to "reviewing scored, document-ready matches" in a single sitting without manual data re-entry between steps.
- **SC-005**: Changing a stated preference (e.g., raising the required salary, or switching from onsite to remote-only) produces a visibly different ranking on the next search, demonstrating the preferences affect scoring.
- **SC-006**: For every selected opening, the user reaches the original live posting to submit manually, and the application performs zero automatic submissions (verifiable: no application is ever sent by the system).
- **SC-007**: The same opening never appears twice in a single results view.
- **SC-008**: When one job source fails, the user still receives results from the remaining sources and is told which source failed.

## Assumptions

- The application is a tool for the resume owner's own job search; it is not a recruiting or employer-facing product.
- The user reviews and is responsible for every document before it is submitted; generated documents are drafts, not final submissions.
- "Opening the posting" means directing the user to the posting's original source; the user completes the employer's own application flow.
- Scoring accuracy improves with richer preference input, which is why salary, work-location, travel, and relocation are collected up front.
- Resume and generated documents contain personal data and are treated as private to the user.
- The 70 threshold and the 1–100 scale are fixed product rules as stated, not user-configurable in the initial scope.
- The application runs as a single-user self-hosted service (like the repository's current deployment model); there are no accounts to manage.
- Enabling automated-browsing discovery of non-API sources is an informed choice made by the resume owner, who accepts the terms-of-service and account-risk consequences; the application's job is to warn clearly and default the mode off, not to enforce a board's policy.
- This repository will be repurposed from its current GitHub→LinkedIn posting tool to this job matcher; the changeover is a planning/migration concern, not part of this feature's user-facing behavior.

## Dependencies

- Access to one or more compliant job-search APIs / aggregators for primary opening discovery.
- The ability to fetch and parse an individual posting from a user-supplied URL.
- An optional automated-browsing capability for non-API sources (e.g. LinkedIn), gated behind an explicit user risk acknowledgment.
- A document-generation capability able to produce a tailored resume and cover letter per opening.

## Out of Scope

- Automatic or one-click submission of applications to employers or job boards.
- Interview scheduling, application status tracking after submission, or recruiter messaging.
- Employer/recruiter-side features (posting jobs, searching candidates).
- Salary negotiation, offer management, or background-check workflows.
