package crawlexecution

// persistence_policy.go owns snapshot cadence and artifact-finalization rules
// for task-state persistence.
//
// Ownership summary:
// 1) define snapshot cadence and force/full/light persistence policy
// 2) centralize artifact-finalization cleanup rules
// 3) keep persistence policy separate from task-state storage mechanics
//
// File map for maintainers:
// 1) snapshot mode and finalization DTOs
// 2) persistence mode resolution helpers
// 3) artifact cleanup and runtime-state retention rules

type SnapshotMode string

const (
	SnapshotModeLight SnapshotMode = "light"
	SnapshotModeFull  SnapshotMode = "full"
)

type ArtifactFinalizationPlan struct {
	ClearUnfinishedReport bool
	CleanupRuntimeState   bool
}

func ResolveSnapshotMode(status string, force bool, explicitMode SnapshotMode) SnapshotMode {
	if explicitMode == SnapshotModeFull || explicitMode == SnapshotModeLight {
		return explicitMode
	}
	if force || IsFinalStatus(status) {
		return SnapshotModeFull
	}
	return SnapshotModeLight
}

func ShouldPersistSnapshot(lastPersistedAtMs int64, nowMs int64, minIntervalMs int64, force bool) bool {
	if force {
		return true
	}
	return nowMs-lastPersistedAtMs >= minIntervalMs
}

func BuildArtifactFinalizationPlan(status string, uncapturedItemsTotal int, recoverablePageAuditCount int) ArtifactFinalizationPlan {
	normalizedStatus := NormalizeStatus(status, "")
	return ArtifactFinalizationPlan{
		ClearUnfinishedReport: normalizedStatus == "completed" && uncapturedItemsTotal == 0 && recoverablePageAuditCount == 0,
		CleanupRuntimeState:   normalizedStatus == "completed",
	}
}
