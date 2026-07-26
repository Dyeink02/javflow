package organizer

// Organizer progress phases are serialized to the frontend and logs. Keep
// these identifiers stable so UI state, troubleshooting notes, and future
// tests do not drift across multiple handwritten string literals.
//
// Ownership summary:
// 1) centralize organizer progress-phase identifiers
// 2) keep expected-code source markers and ad-file actions stable
// 3) avoid duplicated organizer string literals across runtime, bridge, and UI
//
// File map for maintainers:
// 1) organizer progress-phase constants
// 2) expected-code source constants
// 3) ad-file action constants
const (
	progressPhaseStarting        = "starting"
	progressPhaseScanStart       = "scan-start"
	progressPhaseScanProgress    = "scan-progress"
	progressPhaseScanCompleted   = "scan-completed"
	progressPhaseWaitingStart    = "waiting-start"
	progressPhaseWaitingProgress = "waiting-progress"
	progressPhaseDeleteStart     = "delete-start"
	progressPhaseDeleteProgress  = "delete-progress"
	progressPhaseIntroAdStart    = "intro-ad-start"
	progressPhaseIntroAdProgress = "intro-ad-progress"
	progressPhaseCompleted       = "completed"
)

// Organizer reads expected-code inputs from either preloaded payload data or
// stable crawl artifacts. Keep these values centralized because the same source
// types are surfaced in the bridge/UI path and runtime diagnostics.
const (
	codeSourcePayload        = "payload"
	codeSourceFilmData       = "filmData"
	codeSourceOrganizerCodes = "organizerCodes"
)

// Ad file actions are also user-facing config values. Centralizing them here
// reduces the chance of organizer logic, tests, and frontend payloads using
// slightly different spellings.
const (
	adFileActionDeleteDirectly = "delete-directly"
	adFileActionMoveToDelete   = "move-to-delete"
)
