// Package dependencyrules keeps architectural guardrail tests together.
//
// These tests do not enforce business behavior. They enforce the import
// boundaries that keep crawl, organizer, and subscription changes isolated
// enough to debug and refactor safely.
//
// Practical scope:
// 1) organizer should stay on persisted crawl artifacts and local file rules
// 2) avsubscription should stay on persisted artifacts and lightweight target
//    contracts
// 3) crawl execution packages should not drift back into organizer/subscription
//    business domains
//
// Ownership summary:
// 1) collect architectural guardrail tests for module boundaries
// 2) document what import boundaries matter to maintenance and decoupling
// 3) keep dependency-boundary intent explicit alongside the tests
//
// File map for maintainers:
// 1) package-level architectural boundary statement
// 2) module decoupling expectations tied to dependency tests
// 3) rationale for keeping crawl/organizer/subscription imports isolated
package dependencyrules
