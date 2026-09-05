package upgradegate

// The private observer exposes only a closed durability checkpoint. Application
// callers cannot configure it, and it receives no state, SQL or authority. Gate
// subprocess tests use it to pause before an external process kill.
type durableBoundary uint8

const (
	boundaryPrepared durableBoundary = iota + 1
	boundaryWriteStarted
	boundarySQLComplete
	boundarySchemaApplied
	boundaryHealthy
	boundaryHealthFailed
)

func (r Request) observe(point durableBoundary) {
	if r.afterBoundary != nil {
		r.afterBoundary(point)
	}
}
