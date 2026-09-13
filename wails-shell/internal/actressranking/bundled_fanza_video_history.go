// Package actressranking serves normalized actress ranking data from online and
// local sources.
//
// Maintenance boundary:
// - hold only a fixed, verified FANZA Video archive snapshot
// - expose no transport, parser, or mutable user-history behavior
// - preserve the archive's source identity for the bundled-history merger
//
// Ownership summary:
// 1) retain the compressed 2026-01 and 2026-02 FANZA Video Top100 archive
// 2) document the source and calendar-period scope of the payload
// 3) keep this third-party archive distinct from DVD official and AVfan data
//
// File map for maintainers:
// 1) provenance and source-boundary documentation
// 2) compressed immutable local-history payload
package actressranking

// bundledFANZAVideoArchiveBase64 contains two full Top100 snapshots from
// Proclivity-DB's FANZA Video monthly-ranking archive. The source page states
// that FANZA does not retain historical monthly rankings, identifies the
// official Video ranking it records, and labels each page with its month.
// These are deliberately merged as local history rather than as the app's
// DMM DVD or AVfan source, so users can distinguish different ranking lanes.
const bundledFANZAVideoArchiveBase64 = "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"
