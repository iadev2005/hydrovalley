package frames

import (
	"encoding/json"
	"testing"

	playv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/play/v1"
	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
)

const actionID = "0b7c9a52-5f3e-4f5b-9d7e-2a1f3c4d5e6f"

func TestDecodeDrop(t *testing.T) {
	cmd, errMsg := DecodeClient([]byte(`{"type":"drop","action_id":"` + actionID + `","x":14,"y":9}`))
	if errMsg != nil || cmd.Drop == nil || cmd.Admin != nil || cmd.Place != nil || cmd.Buy != nil {
		t.Fatalf("cmd = %+v, error frame: %v", cmd, errMsg)
	}
	drop := cmd.Drop
	if drop.ActionID != actionID || drop.X != 14 || drop.Y != 9 {
		t.Fatalf("drop = %+v", drop)
	}
}

func TestDecodeDropWithZeroCoordinatesOmitted(t *testing.T) {
	cmd, errMsg := DecodeClient([]byte(`{"type":"drop","action_id":"` + actionID + `"}`))
	if errMsg != nil || cmd.Drop == nil {
		t.Fatalf("cmd = %+v, error frame: %v", cmd, errMsg)
	}
	drop := cmd.Drop
	if drop.X != 0 || drop.Y != 0 {
		t.Fatalf("absent coordinates must read as 0, got %+v", drop)
	}
}

func TestDecodeAdminCommands(t *testing.T) {
	cmd, errMsg := DecodeClient([]byte(`{"type":"set_demo_mode","action_id":"` + actionID + `","enabled":true}`))
	if errMsg != nil || cmd.Admin == nil || cmd.Drop != nil {
		t.Fatalf("cmd = %+v, error frame: %v", cmd, errMsg)
	}
	if a := cmd.Admin; a.Kind != AdminSetDemoMode || a.ActionID != actionID || !a.Enabled {
		t.Fatalf("admin = %+v", a)
	}
	// `enabled` ausente se lee como false (protojson omite los ceros).
	cmd, errMsg = DecodeClient([]byte(`{"type":"set_demo_mode","action_id":"` + actionID + `"}`))
	if errMsg != nil || cmd.Admin.Enabled {
		t.Fatalf("cmd = %+v, error frame: %v", cmd, errMsg)
	}
	cmd, errMsg = DecodeClient([]byte(`{"type":"reset_valley","action_id":"` + actionID + `"}`))
	if errMsg != nil || cmd.Admin == nil || cmd.Admin.Kind != AdminResetValley || cmd.Admin.ActionID != actionID {
		t.Fatalf("cmd = %+v, error frame: %v", cmd, errMsg)
	}
}

func TestDecodeAdminErrors(t *testing.T) {
	for _, data := range []string{
		`{"type":"set_demo_mode","enabled":true}`,
		`{"type":"set_demo_mode","action_id":"nope","enabled":true}`,
		`{"type":"set_demo_mode","action_id":"` + actionID + `","enabled":"yes"}`,
		`{"type":"reset_valley","action_id":"` + actionID + `","force":true}`,
	} {
		cmd, errMsg := DecodeClient([]byte(data))
		if cmd.Admin != nil || errMsg == nil || errMsg.GetCode() != CodeInvalidMessage {
			t.Errorf("%s → %+v, %v; want invalid_message", data, cmd, errMsg)
		}
	}
}

func TestDecodePlaceInstallation(t *testing.T) {
	cases := []struct {
		name string
		kind worldv1.InstallationKind
	}{
		{"sprinkler", worldv1.InstallationKind_INSTALLATION_KIND_SPRINKLER},
		{"drip", worldv1.InstallationKind_INSTALLATION_KIND_DRIP},
		{"treatment_plant", worldv1.InstallationKind_INSTALLATION_KIND_TREATMENT_PLANT},
	}
	for _, tc := range cases {
		cmd, errMsg := DecodeClient([]byte(`{"type":"place_installation","action_id":"` + actionID + `","installation":"` + tc.name + `","x":14,"y":9}`))
		if errMsg != nil || cmd.Place == nil || cmd.Drop != nil || cmd.Admin != nil || cmd.Buy != nil {
			t.Fatalf("%s: cmd = %+v, error frame: %v", tc.name, cmd, errMsg)
		}
		if p := cmd.Place; p.ActionID != actionID || p.Kind != tc.kind || p.X != 14 || p.Y != 9 {
			t.Fatalf("%s: place = %+v", tc.name, p)
		}
	}
	// Coordenadas ausentes se leen como 0 (protojson omite los ceros).
	cmd, errMsg := DecodeClient([]byte(`{"type":"place_installation","action_id":"` + actionID + `","installation":"drip"}`))
	if errMsg != nil || cmd.Place == nil || cmd.Place.X != 0 || cmd.Place.Y != 0 {
		t.Fatalf("cmd = %+v, error frame: %v", cmd, errMsg)
	}
}

func TestDecodeBuyUpgrade(t *testing.T) {
	cases := map[string]worldv1.UpgradeKind{
		"bigger_drop": worldv1.UpgradeKind_UPGRADE_KIND_BIGGER_DROP,
		"bigger_tank": worldv1.UpgradeKind_UPGRADE_KIND_BIGGER_TANK,
	}
	for name, kind := range cases {
		cmd, errMsg := DecodeClient([]byte(`{"type":"buy_upgrade","action_id":"` + actionID + `","upgrade":"` + name + `"}`))
		if errMsg != nil || cmd.Buy == nil || cmd.Drop != nil || cmd.Admin != nil || cmd.Place != nil {
			t.Fatalf("%s: cmd = %+v, error frame: %v", name, cmd, errMsg)
		}
		if b := cmd.Buy; b.ActionID != actionID || b.Kind != kind {
			t.Fatalf("%s: buy = %+v", name, b)
		}
	}
}

func TestDecodePurchaseErrors(t *testing.T) {
	for _, data := range []string{
		// sin action_id o con uno que no es UUID
		`{"type":"place_installation","installation":"drip","x":1,"y":1}`,
		`{"type":"place_installation","action_id":"nope","installation":"drip","x":1,"y":1}`,
		`{"type":"buy_upgrade","upgrade":"bigger_drop"}`,
		`{"type":"buy_upgrade","action_id":"nope","upgrade":"bigger_drop"}`,
		// tipo ausente, desconocido, en otro formato o con un valor numérico
		`{"type":"place_installation","action_id":"` + actionID + `","x":1,"y":1}`,
		`{"type":"place_installation","action_id":"` + actionID + `","installation":"fountain","x":1,"y":1}`,
		`{"type":"place_installation","action_id":"` + actionID + `","installation":"Sprinkler","x":1,"y":1}`,
		`{"type":"place_installation","action_id":"` + actionID + `","installation":"INSTALLATION_KIND_DRIP","x":1,"y":1}`,
		`{"type":"place_installation","action_id":"` + actionID + `","installation":2,"x":1,"y":1}`,
		`{"type":"place_installation","action_id":"` + actionID + `","installation":"","x":1,"y":1}`,
		`{"type":"buy_upgrade","action_id":"` + actionID + `"}`,
		`{"type":"buy_upgrade","action_id":"` + actionID + `","upgrade":"bigger_house"}`,
		`{"type":"buy_upgrade","action_id":"` + actionID + `","upgrade":"UPGRADE_KIND_BIGGER_DROP"}`,
		// coordenadas negativas o de texto, campos de otro mensaje o desconocidos
		`{"type":"place_installation","action_id":"` + actionID + `","installation":"drip","x":-1,"y":1}`,
		`{"type":"place_installation","action_id":"` + actionID + `","installation":"drip","x":"1","y":"a"}`,
		`{"type":"place_installation","action_id":"` + actionID + `","upgrade":"bigger_drop"}`,
		`{"type":"buy_upgrade","action_id":"` + actionID + `","upgrade":"bigger_drop","x":1}`,
	} {
		cmd, errMsg := DecodeClient([]byte(data))
		if cmd.Place != nil || cmd.Buy != nil || errMsg == nil || errMsg.GetCode() != CodeInvalidMessage {
			t.Errorf("%s → %+v, %v; want invalid_message", data, cmd, errMsg)
		}
	}
}

func TestPurchaseRejectionReasonsHaveTheirOwnNames(t *testing.T) {
	for reason, want := range map[worldv1.RejectReason]string{
		worldv1.RejectReason_REJECT_REASON_INSUFFICIENT_LEAVES:   "insufficient_leaves",
		worldv1.RejectReason_REJECT_REASON_TILE_OCCUPIED:         "tile_occupied",
		worldv1.RejectReason_REJECT_REASON_UPGRADE_ALREADY_OWNED: "upgrade_already_owned",
		worldv1.RejectReason_REJECT_REASON_INVALID_COMMAND:       "invalid_command",
	} {
		data, _ := Marshal(Result(actionID, StatusRejected, reason))
		if got := decodeJSON(t, data)["reason"]; got != want {
			t.Errorf("reason %v → %v, want %s", reason, got, want)
		}
	}
}

func TestDecodeErrors(t *testing.T) {
	cases := []struct {
		name         string
		data         string
		wantCode     string
		wantActionID string
	}{
		{"invalid json", `{"type":`, CodeInvalidMessage, ""},
		{"not an object", `[1,2]`, CodeInvalidMessage, ""},
		{"missing type", `{"action_id":"` + actionID + `"}`, CodeInvalidMessage, actionID},
		{"type is not a string", `{"type":5}`, CodeInvalidMessage, ""},
		{"unknown type", `{"type":"teleport","action_id":"` + actionID + `"}`, CodeUnknownType, actionID},
		{"server frame sent by client", `{"type":"result","action_id":"` + actionID + `"}`, CodeUnknownType, actionID},
		{"drop without action_id", `{"type":"drop","x":1,"y":2}`, CodeInvalidMessage, ""},
		{"drop with non-uuid action_id", `{"type":"drop","action_id":"abc","x":1}`, CodeInvalidMessage, "abc"},
		{"drop with negative x", `{"type":"drop","action_id":"` + actionID + `","x":-1}`, CodeInvalidMessage, actionID},
		{"drop with text x", `{"type":"drop","action_id":"` + actionID + `","x":"far"}`, CodeInvalidMessage, actionID},
		{"drop with unknown field", `{"type":"drop","action_id":"` + actionID + `","z":3}`, CodeInvalidMessage, actionID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, errMsg := DecodeClient([]byte(tc.data))
			if cmd.Drop != nil || cmd.Admin != nil || cmd.Place != nil || cmd.Buy != nil || errMsg == nil {
				t.Fatalf("got command %+v, want error frame", cmd)
			}
			if errMsg.GetType() != TypeError || errMsg.GetCode() != tc.wantCode || errMsg.GetActionId() != tc.wantActionID {
				t.Fatalf("error frame = %v, want code %q action_id %q", errMsg, tc.wantCode, tc.wantActionID)
			}
		})
	}
}

func TestIsUUID(t *testing.T) {
	valid := []string{actionID, "0B7C9A52-5F3E-4F5B-9D7E-2A1F3C4D5E6F", "00000000-0000-0000-0000-000000000000"}
	invalid := []string{"", "abc", "0b7c9a525f3e4f5b9d7e2a1f3c4d5e6f", "{0b7c9a52-5f3e-4f5b-9d7e-2a1f3c4d5e6f}",
		"0b7c9a52-5f3e-4f5b-9d7e-2a1f3c4d5e6g", "0b7c9a52_5f3e-4f5b-9d7e-2a1f3c4d5e6f"}
	for _, s := range valid {
		if !IsUUID(s) {
			t.Errorf("IsUUID(%q) = false", s)
		}
	}
	for _, s := range invalid {
		if IsUUID(s) {
			t.Errorf("IsUUID(%q) = true", s)
		}
	}
}

func decodeJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("invalid JSON %s: %v", data, err)
	}
	return m
}

func TestMarshalResultUsesProtoNames(t *testing.T) {
	data, err := Marshal(Result(actionID, StatusRejected, worldv1.RejectReason_REJECT_REASON_OUT_OF_BOUNDS))
	if err != nil {
		t.Fatal(err)
	}
	m := decodeJSON(t, data)
	if m["type"] != "result" || m["action_id"] != actionID || m["status"] != "rejected" || m["reason"] != "out_of_bounds" {
		t.Fatalf("result JSON = %s", data)
	}
}

func TestResultWithoutReasonOmitsIt(t *testing.T) {
	data, _ := Marshal(Result(actionID, StatusApplied, worldv1.RejectReason_REJECT_REASON_UNSPECIFIED))
	if _, ok := decodeJSON(t, data)["reason"]; ok {
		t.Fatalf("reason must be omitted: %s", data)
	}
}

func TestReasonNames(t *testing.T) {
	want := map[worldv1.RejectReason]string{
		worldv1.RejectReason_REJECT_REASON_UNSPECIFIED:           "",
		worldv1.RejectReason_REJECT_REASON_INSUFFICIENT_WATER:    "insufficient_water",
		worldv1.RejectReason_REJECT_REASON_INSUFFICIENT_LEAVES:   "insufficient_leaves",
		worldv1.RejectReason_REJECT_REASON_OUT_OF_BOUNDS:         "out_of_bounds",
		worldv1.RejectReason_REJECT_REASON_TILE_OCCUPIED:         "tile_occupied",
		worldv1.RejectReason_REJECT_REASON_UPGRADE_ALREADY_OWNED: "upgrade_already_owned",
		worldv1.RejectReason_REJECT_REASON_INVALID_COMMAND:       "invalid_command",
	}
	for reason, name := range want {
		if got := ReasonName(reason); got != name {
			t.Errorf("ReasonName(%v) = %q, want %q", reason, got, name)
		}
	}
}

func sampleWorld() *worldv1.WorldState {
	tiles := make([]*worldv1.Tile, 4)
	for i := range tiles {
		tiles[i] = &worldv1.Tile{Stage: worldv1.TileStage_TILE_STAGE_DRY_SOIL}
	}
	tiles[0].HasBuilding = true
	tiles[1] = &worldv1.Tile{Stage: worldv1.TileStage_TILE_STAGE_GRASS, Humidity: 25, TrashLevel: 2}
	tiles[2].Fire = &worldv1.Fire{DropsReceived: 2}
	tiles[3].Installation = &worldv1.Installation{Kind: worldv1.InstallationKind_INSTALLATION_KIND_DRIP, OwnerId: "owner"}
	return &worldv1.WorldState{
		Width: 2, Height: 2, Tiles: tiles, DemoMode: true, HumidityContributed: 42,
		Players: []*worldv1.PlayerState{{
			PlayerId: "ana", Water: 19, TankCapacity: 20, Leaves: 3,
			Upgrades: []worldv1.UpgradeKind{worldv1.UpgradeKind_UPGRADE_KIND_BIGGER_TANK},
			NextPrices: []*worldv1.InstallationPrice{
				{Kind: worldv1.InstallationKind_INSTALLATION_KIND_SPRINKLER, Leaves: 10},
				{Kind: worldv1.InstallationKind_INSTALLATION_KIND_DRIP, Leaves: 20},
				{Kind: worldv1.InstallationKind_INSTALLATION_KIND_TREATMENT_PLANT, Leaves: 30},
			},
		}},
	}
}

func TestStateWithMapAndPlayer(t *testing.T) {
	msg := NewStateBuilder(12345678901234567, false, sampleWorld(), 5).For("ana", true)
	if msg.GetType() != TypeState || msg.GetTick() != 12345678901234567 || !msg.GetDemoMode() || msg.GetWidth() != 2 || msg.GetHeight() != 2 {
		t.Fatalf("state header = %v", msg)
	}
	if got := msg.GetStages(); len(got) != 4 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("stages = %v", got)
	}
	if got := msg.GetHumidity(); len(got) != 4 || got[1] != 25 {
		t.Fatalf("humidity = %v", got)
	}
	if got := msg.GetBuildings(); len(got) != 4 || !got[0] || got[1] {
		t.Fatalf("buildings = %v", got)
	}
	if got := msg.GetTrash(); len(got) != 4 || got[1] != 2 {
		t.Fatalf("trash = %v", got)
	}
	if f := msg.GetFires(); len(f) != 1 || f[0].GetX() != 0 || f[0].GetY() != 1 || f[0].GetDropsReceived() != 2 {
		t.Fatalf("fires = %v", f)
	}
	if in := msg.GetInstallations(); len(in) != 1 || in[0].GetX() != 1 || in[0].GetY() != 1 || in[0].GetInstallation() != "drip" || in[0].GetOwnerId() != "owner" {
		t.Fatalf("installations = %v", in)
	}
	p := msg.GetPlayer()
	if p.GetWater() != 19 || p.GetTankCapacity() != 20 || p.GetLeaves() != 3 ||
		len(p.GetUpgrades()) != 1 || p.GetUpgrades()[0] != "bigger_tank" ||
		p.GetNextSprinklerPrice() != 10 || p.GetNextDripPrice() != 20 || p.GetNextTreatmentPlantPrice() != 30 || p.GetNextTrashBinPrice() != 15 {
		t.Fatalf("player = %v", p)
	}
	data, _ := Marshal(msg)
	if tick := decodeJSON(t, data)["tick"]; tick != "12345678901234567" {
		t.Fatalf("tick must travel as text, got %#v", tick)
	}
}

// Los contadores del modo demo (RF-30) viajan como texto (regla 7) y no
// dependen de map=false: el bot de carga y el panel los leen sin el mapa.
func TestStateCarriesDemoCountersAsText(t *testing.T) {
	for _, withMap := range []bool{true, false} {
		msg := NewStateBuilder(9, false, sampleWorld(), 5).For("bob", withMap)
		if msg.GetHumidityContributed() != 42 || msg.GetAcceptedDrops() != 5 {
			t.Fatalf("withMap=%v counters = %d/%d, want 42/5", withMap, msg.GetHumidityContributed(), msg.GetAcceptedDrops())
		}
		data, _ := Marshal(msg)
		got := decodeJSON(t, data)
		if got["humidity_contributed"] != "42" || got["accepted_drops"] != "5" {
			t.Fatalf("withMap=%v counters must travel as text, got %#v / %#v", withMap, got["humidity_contributed"], got["accepted_drops"])
		}
	}
}

// Sin mundo todavía no hay humedad que contar, pero las gotas aceptadas
// son del hub y siempre se informan.
func TestStateWithoutWorldStillReportsAcceptedDrops(t *testing.T) {
	msg := NewStateBuilder(0, true, nil, 3).For("ana", true)
	if msg.GetAcceptedDrops() != 3 || msg.GetHumidityContributed() != 0 {
		t.Fatalf("counters = %d/%d, want 0 humidity and 3 drops", msg.GetHumidityContributed(), msg.GetAcceptedDrops())
	}
}

func TestStateWithoutMap(t *testing.T) {
	msg := NewStateBuilder(7, true, sampleWorld(), 5).For("ana", false)
	if !msg.GetPaused() || msg.GetWidth() != 2 {
		t.Fatalf("state = %v", msg)
	}
	if len(msg.GetStages())+len(msg.GetHumidity())+len(msg.GetFires())+len(msg.GetInstallations()) != 0 {
		t.Fatalf("map=false must skip the map fields: %v", msg)
	}
	if msg.GetPlayer().GetWater() != 19 {
		t.Fatal("map=false keeps the player view")
	}
}

func TestStateForUnknownPlayerAndNoWorld(t *testing.T) {
	msg := NewStateBuilder(0, true, sampleWorld(), 0).For("bob", true)
	if msg.GetPlayer() != nil {
		t.Fatal("player absent from the world must have no player view")
	}
	empty := NewStateBuilder(0, true, nil, 0).For("ana", true)
	if empty.GetType() != TypeState || !empty.GetPaused() || empty.GetPlayer() != nil || len(empty.GetStages()) != 0 {
		t.Fatalf("state without world = %v", empty)
	}
	var _ *playv1.StateMessage = empty
}

func worldWithSummary(summary *worldv1.OfflineSummary) *worldv1.WorldState {
	return &worldv1.WorldState{
		Width: 2, Height: 2,
		Players: []*worldv1.PlayerState{{PlayerId: "ana", Water: 1, OfflineSummary: summary}},
	}
}

// El resumen al volver (RF-25) no viaja en el For normal: el hub lo adjunta
// solo al primer estado de cada conexión.
func TestStateOmitsTheOfflineSummaryUnlessAttached(t *testing.T) {
	world := worldWithSummary(&worldv1.OfflineSummary{TilesGrown: 3})
	msg := NewStateBuilder(1, false, world, 0).For("ana", false)
	if msg.GetPlayer().GetOfflineSummary() != nil {
		t.Fatalf("For must not carry the summary, got %v", msg.GetPlayer().GetOfflineSummary())
	}
}

func TestAttachOfflineSummaryMapsTheCountsAndTravelsAsText(t *testing.T) {
	world := worldWithSummary(&worldv1.OfflineSummary{
		LeavesEarned: 12, WaterReused: 1 << 40, FiresExtinguished: 1, TilesGrown: 38,
	})
	builder := NewStateBuilder(1, false, world, 0)
	msg := builder.For("ana", false)
	if !builder.AttachOfflineSummary(msg, "ana") {
		t.Fatal("the summary was not attached")
	}
	got := msg.GetPlayer().GetOfflineSummary()
	if got.GetLeavesEarned() != 12 || got.GetWaterReused() != 1<<40 || got.GetFiresExtinguished() != 1 || got.GetTilesGrown() != 38 {
		t.Fatalf("summary = %v", got)
	}
	data, _ := Marshal(msg)
	summary := decodeJSON(t, data)["player"].(map[string]any)["offline_summary"].(map[string]any)
	if summary["tiles_grown"] != "38" || summary["water_reused"] != "1099511627776" {
		t.Fatalf("counters must travel as text, got %#v", summary)
	}
}

func TestAttachOfflineSummaryDoesNothingWhenThereIsNothingToShow(t *testing.T) {
	for name, summary := range map[string]*worldv1.OfflineSummary{"missing": nil, "empty": {}} {
		builder := NewStateBuilder(1, false, worldWithSummary(summary), 0)
		msg := builder.For("ana", false)
		if builder.AttachOfflineSummary(msg, "ana") || msg.GetPlayer().GetOfflineSummary() != nil {
			t.Errorf("%s: a summary was attached", name)
		}
	}
	// Sin jugador en el mundo tampoco hay nada que adjuntar.
	builder := NewStateBuilder(1, false, worldWithSummary(&worldv1.OfflineSummary{TilesGrown: 1}), 0)
	if builder.AttachOfflineSummary(builder.For("bob", false), "bob") {
		t.Error("attached a summary for a player that is not in the world")
	}
}
