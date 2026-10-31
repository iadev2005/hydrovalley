package domain

// Reglas de una casilla: crecimiento, evaporación y marchitamiento
// (diagrama 05, región «Crecimiento»). Port de growth.rs.
//
// Funciones puras sobre una Tile, con enteros sin signo y sin azar. El World
// decide cuándo llamarlas (ver el orden del tick en World.Step) y reparte las
// hojas.

// AddHumidity suma amount de humedad y hace crecer la casilla las veces que
// alcance. Devuelve las hojas ganadas (la suma de las de cada etapa
// alcanzada).
//
// Cada ascenso gasta HumidityPerStage. Con las cifras de D-01 una gota sube
// como mucho una etapa; el bucle solo cubre un catálogo con cifras que
// permitan más. En árbol la humedad queda limitada a TreeMaxHumidity. Una
// casilla ardiendo guarda la humedad pero no crece (guarda [sin fuego] del
// diagrama 05).
func AddHumidity(tile *Tile, amount uint64, growth GrowthRules) uint64 {
	tile.Humidity = satAdd(tile.Humidity, amount)
	if tile.Fire != nil {
		return 0
	}
	var leaves uint64
	for {
		next, ok := tile.Stage.Next()
		if !ok || tile.Humidity < growth.HumidityPerStage {
			break
		}
		tile.Humidity -= growth.HumidityPerStage
		tile.Stage = next
		tile.DryTicks = 0
		leaves = satAdd(leaves, growth.LeavesFor(next))
	}
	if tile.Stage == Tree {
		tile.Humidity = minU64(tile.Humidity, growth.TreeMaxHumidity)
	}
	return leaves
}

// Evaporate resta amount de humedad, sin bajar de 0. También una casilla
// ardiendo: el fuego congela la etapa, no la evaporación (diagrama 05).
func Evaporate(tile *Tile, amount uint64) {
	tile.Humidity = satSub(tile.Humidity, amount)
}

// Wither avanza un tick el marchitamiento. Devuelve true si la casilla bajó
// de etapa.
//
// DryTicks cuenta los ticks seguidos con humedad 0 de una planta (tierra
// seca no tiene qué marchitar); cualquier humedad lo reinicia. Al llegar a
// WitherAfterTicks baja una etapa y vuelve a contar desde 0. Una casilla
// ardiendo queda congelada: ni cuenta ni se reinicia.
func Wither(tile *Tile, rules EvaporationRules) bool {
	if tile.Fire != nil {
		return false
	}
	lower, ok := tile.Stage.Previous()
	if !ok {
		tile.DryTicks = 0
		return false
	}
	if tile.Humidity > 0 {
		tile.DryTicks = 0
		return false
	}
	tile.DryTicks = satAdd32(tile.DryTicks, 1)
	if tile.DryTicks < rules.WitherAfterTicks {
		return false
	}
	tile.Stage = lower
	tile.DryTicks = 0
	return true
}
