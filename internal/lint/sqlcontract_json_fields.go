package lint

// Legacy adapter worker JSON carriers, reviewed for each exact query.
var jsonQueryFields = map[string]map[string]bool{
	"AdapterWorkerActivateApplyTarget":                 {"SelectedRaw": true},
	"AdapterWorkerActivateOriginRouteMoveApplyTarget":  {"SelectedRaw": true},
	"AdapterWorkerFinishDeadCredentialScrubMarkTarget": {"FailureJSON": true},
	"AdapterWorkerFinishJobQuery3":                     {"FailureJSON": true, "WarningJSON": true},
	"AdapterWorkerFinishOriginRouteMoveScrubMarkDone":  {"FailureJSON": true},
	"AdapterWorkerFinishRouteMoveScrubMark":            {"FailureJSON": true},
	"AdapterWorkerFinishRouteMoveScrubPersistOrphans":  {"OrphanJSON": true},
}
