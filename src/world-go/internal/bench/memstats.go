package bench

import "runtime"

// runtimeStats guarda los ciclos del recolector, solo como dato informativo.
type runtimeStats struct{ numGC uint32 }

func (r *runtimeStats) read() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	r.numGC = m.NumGC
}
