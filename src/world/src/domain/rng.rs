//! Generador de números pseudoaleatorios con semilla (regla 6 de
//! `CLAUDE.md`): SplitMix64.
//!
//! Se eligió SplitMix64 y no una dependencia (`rand_chacha`) porque el
//! algoritmo cabe en tres líneas, su estado es un solo `u64` que viaja en la
//! instantánea (`WorldSnapshot.rng_state`, 8 bytes) y la secuencia depende
//! solo de la semilla y de la aritmética entera: no cambia con la versión de
//! ninguna biblioteca ni de la plataforma. Es un generador de uso general,
//! no criptográfico; aquí solo decide dónde arden los incendios. Los
//! vectores de las pruebas son los de referencia de Vigna (semilla 0).

/// Longitud en bytes del estado serializado.
pub const STATE_LEN: usize = 8;

const GAMMA: u64 = 0x9E37_79B9_7F4A_7C15;

/// Partes por millón que valen el 100 %.
pub const PPM_SCALE: u64 = 1_000_000;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct SplitMix64 {
    state: u64,
}

impl SplitMix64 {
    pub fn new(seed: u64) -> Self {
        Self { state: seed }
    }

    pub fn next_u64(&mut self) -> u64 {
        self.state = self.state.wrapping_add(GAMMA);
        let mut z = self.state;
        z = (z ^ (z >> 30)).wrapping_mul(0xBF58_476D_1CE4_E5B9);
        z = (z ^ (z >> 27)).wrapping_mul(0x94D0_49BB_1331_11EB);
        z ^ (z >> 31)
    }

    /// Un sorteo con probabilidad `ppm` partes por millón (0 nunca, un
    /// millón o más siempre). Consume exactamente un número, también cuando
    /// la probabilidad es 0 o 100 %, para que la secuencia no dependa del
    /// resultado.
    pub fn chance_ppm(&mut self, ppm: u32) -> bool {
        // Multiplicación de alta parte: reparte el u64 en [0, 1.000.000) sin
        // división ni sesgo apreciable (menos de 1e-13).
        let scaled = (u128::from(self.next_u64()) * u128::from(PPM_SCALE)) >> 64;
        scaled < u128::from(ppm)
    }

    /// Estado serializado: el `u64` en little-endian.
    pub fn to_bytes(self) -> Vec<u8> {
        self.state.to_le_bytes().to_vec()
    }

    /// `None` si no mide exactamente [`STATE_LEN`] bytes.
    pub fn from_bytes(bytes: &[u8]) -> Option<Self> {
        let array: [u8; STATE_LEN] = bytes.try_into().ok()?;
        Some(Self {
            state: u64::from_le_bytes(array),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn it_matches_the_reference_vectors_for_seed_zero() {
        let mut rng = SplitMix64::new(0);
        assert_eq!(rng.next_u64(), 0xE220_A839_7B1D_CDAF);
        assert_eq!(rng.next_u64(), 0x6E78_9E6A_A1B9_65F4);
        assert_eq!(rng.next_u64(), 0x06C4_5D18_8009_454F);
    }

    #[test]
    fn the_same_seed_gives_the_same_sequence_and_another_seed_a_different_one() {
        let take = |seed| {
            let mut rng = SplitMix64::new(seed);
            (0..8).map(|_| rng.next_u64()).collect::<Vec<_>>()
        };
        assert_eq!(take(42), take(42));
        assert_ne!(take(42), take(43));
    }

    #[test]
    fn the_state_round_trips_and_continues_the_sequence() {
        let mut rng = SplitMix64::new(7);
        rng.next_u64();
        let bytes = rng.to_bytes();
        assert_eq!(bytes.len(), STATE_LEN);
        let mut restored = SplitMix64::from_bytes(&bytes).unwrap();
        assert_eq!(restored, rng);
        assert_eq!(restored.next_u64(), rng.next_u64());
    }

    #[test]
    fn a_state_of_the_wrong_length_is_refused() {
        assert!(SplitMix64::from_bytes(&[]).is_none());
        assert!(SplitMix64::from_bytes(&[1, 2, 3]).is_none());
        assert!(SplitMix64::from_bytes(&[0; 9]).is_none());
    }

    #[test]
    fn the_chance_is_never_with_zero_and_always_with_a_million() {
        let mut rng = SplitMix64::new(1);
        assert!((0..1000).all(|_| !rng.chance_ppm(0)));
        assert!((0..1000).all(|_| rng.chance_ppm(1_000_000)));
    }

    #[test]
    fn every_chance_consumes_exactly_one_number() {
        let mut a = SplitMix64::new(9);
        let mut b = SplitMix64::new(9);
        a.chance_ppm(0);
        a.chance_ppm(500_000);
        b.next_u64();
        b.next_u64();
        assert_eq!(a, b);
    }

    #[test]
    fn the_chance_is_close_to_the_requested_probability() {
        let mut rng = SplitMix64::new(2026);
        let hits = (0..200_000).filter(|_| rng.chance_ppm(100_000)).count();
        // 10 % de 200.000 = 20.000; margen amplio (~7 desviaciones típicas).
        assert!((19_000..=21_000).contains(&hits), "hits = {hits}");
    }
}
