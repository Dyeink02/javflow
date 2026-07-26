// Package organizer is the current Go-native video organizer domain.
//
// It should remain independent from live crawler controller state and consume
// persisted crawl artifacts or explicit inputs only. That boundary keeps file
// organization, post-download troubleshooting, and future module changes
// localized instead of coupling them back to the crawler UI flow.
//
// Ownership summary:
// 1) expose the stable Go-native organizer domain facade
// 2) keep live crawler controller state out of organizer domain entrypoints
// 3) leave orchestration/path/report details in dedicated sibling files
//
// File map for maintainers:
// 1) organizer facade struct
// 2) facade constructor
package organizer

import runtimepaths "javflow/internal/runtime"

// Service is the stable facade for the video organizer domain.
//
// Keep this file intentionally small so service ownership stays obvious while
// detailed responsibilities live in path, report, and run-focused files.
//
// Practical rule:
// - public bridge callers depend on `Service`
// - orchestration lives in `run.go`
// - path/report/load helpers stay split into sibling files
// - avoid turning this file into a second orchestration surface
type Service struct {
	paths runtimepaths.Paths
}

func NewService(paths ...runtimepaths.Paths) *Service {
	// The facade constructor stays empty on purpose. Runtime state and execution
	// envelopes belong to dedicated run/report/path helpers, not this shell.
	service := &Service{}
	if len(paths) > 0 {
		service.paths = paths[0]
	}
	return service
}
