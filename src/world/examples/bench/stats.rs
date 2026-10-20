//! Estadísticas de los tiempos por paso, en microsegundos.
//!
//! Definiciones (idénticas en `src/world-go/internal/bench`): la media es la
//! aritmética; la mediana es el valor central (el promedio de los dos
//! centrales si hay un número par de muestras); p95 y p99 usan el rango más
//! cercano: el valor de posición `ceil(p * n / 100)` con las muestras
//! ordenadas.

/// Resumen de una serie de tiempos.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Stats {
    pub count: usize,
    pub mean_us: f64,
    pub median_us: f64,
    pub p95_us: f64,
    pub p99_us: f64,
    pub min_us: f64,
    pub max_us: f64,
}

/// Resume `samples_ns` (nanosegundos, en cualquier orden); `None` si no hay
/// muestras.
pub fn summarize(samples_ns: &[u64]) -> Option<Stats> {
    if samples_ns.is_empty() {
        return None;
    }
    let mut sorted = samples_ns.to_vec();
    sorted.sort_unstable();
    let n = sorted.len();
    let to_us = |ns: f64| ns / 1000.0;
    let sum: u128 = sorted.iter().map(|&ns| u128::from(ns)).sum();
    let median = if n % 2 == 1 {
        sorted[n / 2] as f64
    } else {
        (sorted[n / 2 - 1] as f64 + sorted[n / 2] as f64) / 2.0
    };
    let nearest_rank = |percent: usize| sorted[(percent * n).div_ceil(100).max(1) - 1] as f64;
    Some(Stats {
        count: n,
        mean_us: to_us(sum as f64 / n as f64),
        median_us: to_us(median),
        p95_us: to_us(nearest_rank(95)),
        p99_us: to_us(nearest_rank(99)),
        min_us: to_us(sorted[0] as f64),
        max_us: to_us(sorted[n - 1] as f64),
    })
}
