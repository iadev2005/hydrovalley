package bench

import "slices"

// Stats es el resumen de una serie de tiempos por paso, en microsegundos.
//
// Definiciones (idénticas a las de src/world/examples/bench/stats.rs): la
// media es la aritmética; la mediana es el valor central (el promedio de los
// dos centrales si hay un número par de muestras); p95 y p99 usan el rango
// más cercano: el valor de posición ceil(p * n / 100) con las muestras
// ordenadas.
type Stats struct {
	Count                          int
	MeanUS, MedianUS, P95US, P99US float64
	MinUS, MaxUS                   float64
}

// Summarize resume samplesNS (nanosegundos, en cualquier orden) sin
// modificarlo; ok es false si no hay muestras.
func Summarize(samplesNS []int64) (stats Stats, ok bool) {
	n := len(samplesNS)
	if n == 0 {
		return Stats{}, false
	}
	sorted := slices.Clone(samplesNS)
	slices.Sort(sorted)
	var total int64
	for _, ns := range sorted {
		total += ns
	}
	sum := float64(total)
	var median float64
	if n%2 == 1 {
		median = float64(sorted[n/2])
	} else {
		median = (float64(sorted[n/2-1]) + float64(sorted[n/2])) / 2
	}
	nearestRank := func(percent int) float64 {
		rank := max((percent*n+99)/100, 1)
		return float64(sorted[rank-1])
	}
	toUS := func(ns float64) float64 { return ns / 1000 }
	return Stats{
		Count:    n,
		MeanUS:   toUS(sum / float64(n)),
		MedianUS: toUS(median),
		P95US:    toUS(nearestRank(95)),
		P99US:    toUS(nearestRank(99)),
		MinUS:    toUS(float64(sorted[0])),
		MaxUS:    toUS(float64(sorted[n-1])),
	}, true
}
