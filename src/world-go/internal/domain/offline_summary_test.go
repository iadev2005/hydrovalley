package domain

import "testing"

// Portadas de las pruebas del resumen al volver de world.rs (RF-25): lo que
// hicieron los aspersores de un jugador desde su última acción aplicada.

// worldWithASprinklerAndAFire es un aspersor de Ana en (5, 5), con pasto en
// (5, 6) a punto de crecer (90 de 100 de humedad: el primer riego de 25 lo
// sube de etapa) y un fuego en (6, 5). Devuelve el mundo justo después de
// colocarlo.
func worldWithASprinklerAndAFire(t *testing.T) *World {
	t.Helper()
	w := rich(holder{ana, 100})
	w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 5, 5)})
	grass := mustTile(t, w, 5, 6)
	grass.Stage = Grass
	grass.Humidity = 90
	mustTile(t, w, 6, 5).Fire = &Fire{DropsReceived: 0, BurningTicks: 10}
	return w
}

func TestTheSummaryCountsWhatTheSprinklersDidAfterTheLastAction(t *testing.T) {
	w := worldWithASprinklerAndAFire(t)
	if got := mustPlayer(t, w, ana).OfflineSummary; !got.IsEmpty() {
		t.Fatalf("summary before any watering = %+v, want empty", got)
	}
	advanceTo(t, w, 20)
	player := mustPlayer(t, w, ana)
	summary := player.OfflineSummary
	// Un riego: 25 de humedad en las 9 casillas del cuadrado.
	if summary.WaterReused != 9*25 {
		t.Errorf("water reused = %d, want %d", summary.WaterReused, 9*25)
	}
	if summary.FiresExtinguished != 1 {
		t.Errorf("fires extinguished = %d, want 1", summary.FiresExtinguished)
	}
	// Solo crece el pasto en (5, 6); la casilla del fuego no llega al umbral.
	if summary.TilesGrown != 1 || player.TilesGrown != 1 {
		t.Errorf("tiles grown = %d/%d, want 1/1", summary.TilesGrown, player.TilesGrown)
	}
	// `rich` da 100 hojas de entrada, que no cuentan como ganadas por riego.
	if summary.LeavesEarned == 0 || summary.LeavesEarned != player.LeavesEarned-100 {
		t.Errorf("leaves earned = %d, want %d", summary.LeavesEarned, player.LeavesEarned-100)
	}
}

func TestTheSummaryKeepsAddingUpAndTheNextAppliedActionClearsIt(t *testing.T) {
	w := worldWithASprinklerAndAFire(t)
	advanceTo(t, w, 20)
	first := mustPlayer(t, w, ana).OfflineSummary
	advanceTo(t, w, 40)
	second := mustPlayer(t, w, ana).OfflineSummary
	if second.WaterReused != first.WaterReused+9*25 {
		t.Errorf("water reused = %d, want %d", second.WaterReused, first.WaterReused+9*25)
	}

	w.Step([]Command{dropCmd(action(2), ana, 0, 0)})
	player := mustPlayer(t, w, ana)
	if !player.OfflineSummary.IsEmpty() {
		t.Errorf("summary after an action = %+v, want empty", player.OfflineSummary)
	}
	// El acumulado de la época no se borra con la acción.
	if player.FiresExtinguished != 1 || player.TilesGrown < 1 {
		t.Errorf("cumulative = %d/%d, want 1/>=1", player.FiresExtinguished, player.TilesGrown)
	}
}

func TestARejectedActionDoesNotClearTheSummary(t *testing.T) {
	w := worldWithASprinklerAndAFire(t)
	advanceTo(t, w, 20)
	// Fuera del valle: se rechaza y no cuenta como acción.
	w.Step([]Command{dropCmd(action(2), ana, 99, 99)})
	if mustPlayer(t, w, ana).OfflineSummary.IsEmpty() {
		t.Error("a rejected action cleared the summary")
	}
}

func TestASprinklerEffectInTheTickOfTheActionCountsAfterIt(t *testing.T) {
	w := worldWithASprinklerAndAFire(t)
	advanceTo(t, w, 19)
	// En el tick 20 Ana actúa y el aspersor riega después de su comando.
	w.Step([]Command{dropCmd(action(2), ana, 0, 0)})
	player := mustPlayer(t, w, ana)
	if player.LastActionTick != 20 || player.OfflineSummary.WaterReused != 9*25 {
		t.Errorf("last action %d, water reused %d; want 20 and %d",
			player.LastActionTick, player.OfflineSummary.WaterReused, 9*25)
	}
}

func TestDripAndTreatmentPlantsDoNotFeedTheSummary(t *testing.T) {
	w := rich(holder{ana, 100})
	w.Step([]Command{
		placeCmd(action(1), ana, Drip, 10, 10),
		placeCmd(action(2), ana, TreatmentPlant, 1, 1),
	})
	// Con el depósito lleno la planta no recicla nada: se gasta antes.
	for n := uint32(3); n < 8; n++ {
		w.Step([]Command{dropCmd(action(n), ana, 20, 20)})
	}
	setHumidity(t, w, 10, 10, 10)
	advanceTo(t, w, 100)
	player := mustPlayer(t, w, ana)
	if player.WaterReduced == 0 || player.WaterRecycled == 0 {
		t.Fatalf("reduced %d, recycled %d; both should be > 0", player.WaterReduced, player.WaterRecycled)
	}
	if !player.OfflineSummary.IsEmpty() {
		t.Errorf("summary = %+v, want empty", player.OfflineSummary)
	}
}

func TestTheSummaryStopsGrowingWhenTheSprinklersStop(t *testing.T) {
	w := worldWithASprinklerAndAFire(t)
	// Pasadas las 2 h sin acciones el aspersor se para (RF-25).
	advanceTo(t, w, 72_001+20)
	frozen := mustPlayer(t, w, ana).OfflineSummary
	if frozen.IsEmpty() {
		t.Fatal("the summary should not be empty")
	}
	advanceTo(t, w, 72_001+100)
	if got := mustPlayer(t, w, ana).OfflineSummary; got != frozen {
		t.Errorf("summary = %+v, want it frozen at %+v", got, frozen)
	}
}

func TestResetValleyClearsTheSummaryAndTheSprinklerCounters(t *testing.T) {
	w := worldWithASprinklerAndAFire(t)
	advanceTo(t, w, 20)
	w.Step([]Command{resetCmd(action(2))})
	player := mustPlayer(t, w, ana)
	if !player.OfflineSummary.IsEmpty() || player.FiresExtinguished != 0 || player.TilesGrown != 0 {
		t.Errorf("after the reset: %+v, %d, %d", player.OfflineSummary, player.FiresExtinguished, player.TilesGrown)
	}
}

func TestTheSummarySurvivesAPartsAndRestoreRoundTrip(t *testing.T) {
	w := worldWithASprinklerAndAFire(t)
	advanceTo(t, w, 20)
	restored := mustRestore(t, d01(), w.Parts())
	want := mustPlayer(t, w, ana)
	got := mustPlayer(t, restored, ana)
	if got.OfflineSummary != want.OfflineSummary || got.FiresExtinguished != want.FiresExtinguished || got.TilesGrown != want.TilesGrown {
		t.Errorf("restored = %+v, want %+v", got, want)
	}
}
