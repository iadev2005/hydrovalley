package domain

// Reglas de una casilla que arde (diagrama 05, región «Fuego»). Port de
// fire.rs.
//
// Funciones puras sobre una Tile, sin azar: el sorteo de la ignición lo hace
// el World con su generador (T4) y solo llama a Ignite cuando sale. Mientras
// arde, el crecimiento y el marchitamiento quedan congelados (ver growth.go),
// pero la evaporación sigue.

// IsEligible dice si la casilla puede arder: con planta, sin humedad y que no
// arde ya.
func IsEligible(tile *Tile) bool {
	return tile.Stage != DrySoil && tile.Humidity == 0 && tile.Fire == nil
}

// Ignite enciende la casilla: empieza sin gotas y con la cuenta de ticks a 0.
func Ignite(tile *Tile) {
	tile.Fire = &Fire{}
}

// ReceiveDrop cuenta una gota recibida por una casilla ardiendo. Con la que
// completa DropsToExtinguish el fuego se apaga y devuelve true; las gotas que
// llegan después ya no encuentran fuego. Una casilla que no arde no cuenta
// nada y devuelve false.
func ReceiveDrop(tile *Tile, rules FireRules) bool {
	fire := tile.Fire
	if fire == nil {
		return false
	}
	fire.DropsReceived = satAdd32(fire.DropsReceived, 1)
	if fire.DropsReceived >= rules.DropsToExtinguish {
		tile.Fire = nil
		return true
	}
	return false
}

// Extinguish apaga el fuego (aspersor). Devuelve true si ardía.
func Extinguish(tile *Tile) bool {
	burning := tile.Fire != nil
	tile.Fire = nil
	return burning
}

// Burn cuenta un tick más de fuego. Al llegar a BurnOutTicks se consume: el
// fuego termina y la casilla vuelve a tierra seca desde cualquier etapa, con
// la humedad y la instalación intactas. Devuelve true si se consumió.
func Burn(tile *Tile, rules FireRules) bool {
	fire := tile.Fire
	if fire == nil {
		return false
	}
	fire.BurningTicks = satAdd32(fire.BurningTicks, 1)
	if fire.BurningTicks < rules.BurnOutTicks {
		return false
	}
	tile.Fire = nil
	tile.Stage = DrySoil
	tile.DryTicks = 0
	return true
}
