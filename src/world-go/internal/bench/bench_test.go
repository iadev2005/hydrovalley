package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/domain"
)

// root es la raíz del repositorio (este paquete vive en src/world-go/internal).
const root = "../../../.."

var uuidText = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func workloadFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "tests", "bench", "workloads", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func small(t *testing.T) Workload {
	t.Helper()
	w, err := ParseWorkload([]byte(`{"name": "t", "seed": 7, "width": 8, "height": 8, "players": 3,
		"warmup_ticks": 10, "measured_ticks": 5,
		"drops_per_1000_tiles": 250, "hot_percent": 50,
		"installations_per_1000_tiles": 100,
		"catalog_file": "catalog/catalog.json", "catalog_set": {}}`))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func describe(c domain.Command) string {
	var kind string
	switch k := c.Kind.(type) {
	case domain.DropCommand:
		kind = fmt.Sprintf("D %d %d", k.X, k.Y)
	case domain.PlaceInstallationCommand:
		kind = fmt.Sprintf("P %d %d %d", k.Kind, k.X, k.Y)
	default:
		panic(fmt.Sprintf("la carga no genera %T", k))
	}
	return fmt.Sprintf("%s %s %s\n", c.ActionID, c.PlayerID, kind)
}

func TestTheWorkloadReadsItsParametersAndDerivesThePerTickLoad(t *testing.T) {
	w := small(t)
	if w.Width != 8 || w.Height != 8 || w.Players != 3 || w.WarmupTicks != 10 || w.MeasuredTicks != 5 {
		t.Errorf("unexpected workload %+v", w)
	}
	if got := w.DropsPerTick(); got != 16 { // 64 casillas * 250 / 1000
		t.Errorf("drops per tick = %d, want 16", got)
	}
	if got := w.InstallationsTotal(); got != 6 { // 64 * 100 / 1000
		t.Errorf("installations = %d, want 6", got)
	}
}

func TestAWorkloadWithAMissingOrImpossibleFieldIsRefused(t *testing.T) {
	if _, err := ParseWorkload([]byte(`{}`)); err == nil {
		t.Error("an empty workload was accepted")
	}
	text := `{"name": "t", "seed": 7, "width": 0, "height": 8, "players": 3,
		"warmup_ticks": 10, "measured_ticks": 5, "drops_per_1000_tiles": 1,
		"hot_percent": 50, "installations_per_1000_tiles": 1,
		"catalog_file": "c", "catalog_set": {}}`
	if _, err := ParseWorkload([]byte(text)); err == nil {
		t.Error("a zero width was accepted")
	}
	over := strings.NewReplacer(`"width": 0`, `"width": 8`, "50", "101").Replace(text)
	if _, err := ParseWorkload([]byte(over)); err == nil {
		t.Error("hot_percent over 100 was accepted")
	}
}

func TestTheSameWorkloadAlwaysGeneratesTheSameCommands(t *testing.T) {
	w := small(t)
	a, b := NewGenerator(w), NewGenerator(w)
	for tick := uint64(1); tick <= 30; tick++ {
		if x, y := fmt.Sprint(a.Commands(tick)), fmt.Sprint(b.Commands(tick)); x != y {
			t.Fatalf("tick %d differs", tick)
		}
	}
}

func TestEveryTickHasTheScaledNumberOfDropsInsideTheValley(t *testing.T) {
	w := small(t)
	g := NewGenerator(w)
	for tick := uint64(1); tick <= 30; tick++ {
		drops := 0
		for _, c := range g.Commands(tick) {
			if d, ok := c.Kind.(domain.DropCommand); ok {
				drops++
				if d.X >= 8 || d.Y >= 8 {
					t.Fatalf("drop outside the valley: %+v", d)
				}
			}
		}
		if uint64(drops) != w.DropsPerTick() {
			t.Fatalf("tick %d: %d drops, want %d", tick, drops, w.DropsPerTick())
		}
	}
}

func TestInstallationsArePlacedOnlyInTheSecondHalfOfTheWarmUp(t *testing.T) {
	w := small(t)
	g := NewGenerator(w)
	var perKind [3]int
	for tick := uint64(1); tick <= 30; tick++ {
		for _, c := range g.Commands(tick) {
			p, ok := c.Kind.(domain.PlaceInstallationCommand)
			if !ok {
				continue
			}
			if tick < 6 || tick > 10 {
				t.Errorf("placement out of the window at tick %d", tick)
			}
			perKind[p.Kind]++
		}
	}
	if perKind != [3]int{2, 2, 2} {
		t.Errorf("per kind = %v, want [2 2 2]", perKind)
	}
}

func TestIDsAreUniqueValidUUIDsAndPlayersStayWithinTheFixedSet(t *testing.T) {
	w := small(t)
	g := NewGenerator(w)
	actions := map[string]bool{}
	players := map[string]bool{}
	for tick := uint64(1); tick <= 30; tick++ {
		for _, c := range g.Commands(tick) {
			if !uuidText.MatchString(c.ActionID) || !uuidText.MatchString(c.PlayerID) {
				t.Fatalf("not a UUID: %+v", c)
			}
			if actions[c.ActionID] {
				t.Fatalf("repeated action %s", c.ActionID)
			}
			actions[c.ActionID] = true
			players[c.PlayerID] = true
		}
	}
	if len(players) != 3 {
		t.Errorf("%d players, want 3", len(players))
	}
}

func TestDropsConcentrateOnTheCentralQuarterAsConfigured(t *testing.T) {
	w, err := ParseWorkload([]byte(`{"name": "t", "seed": 1, "width": 32, "height": 32, "players": 4,
		"warmup_ticks": 2, "measured_ticks": 2, "drops_per_1000_tiles": 1000,
		"hot_percent": 100, "installations_per_1000_tiles": 0,
		"catalog_file": "c", "catalog_set": {}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range NewGenerator(w).Commands(1) {
		d := c.Kind.(domain.DropCommand)
		if d.X < 8 || d.X >= 24 || d.Y < 8 || d.Y >= 24 {
			t.Fatalf("(%d, %d) is outside the central quarter", d.X, d.Y)
		}
	}
}

// valley32CommandsFNV1a es la huella de los comandos de los 250 primeros
// ticks de valley-32.json. El mismo valor está en la prueba de Rust
// (src/world/tests/bench_support.rs): si los dos generadores dejan de
// coincidir, falla uno de los dos.
const valley32CommandsFNV1a uint64 = 0x900e8f411a60993b

func TestTheCommandStreamOfValley32MatchesTheGoldenFingerprint(t *testing.T) {
	w, err := ParseWorkload(workloadFile(t, "valley-32.json"))
	if err != nil {
		t.Fatal(err)
	}
	g := NewGenerator(w)
	var text strings.Builder
	for tick := uint64(1); tick <= 250; tick++ {
		for _, c := range g.Commands(tick) {
			text.WriteString(describe(c))
		}
	}
	if got := fnv1a([]byte(text.String())); got != valley32CommandsFNV1a {
		t.Errorf("fingerprint = %#x, want %#x", got, valley32CommandsFNV1a)
	}
}

func TestEveryCommittedWorkloadIsValid(t *testing.T) {
	for _, size := range []uint32{32, 64, 128, 256, 512} {
		w, err := ParseWorkload(workloadFile(t, fmt.Sprintf("valley-%d.json", size)))
		if err != nil {
			t.Fatal(err)
		}
		if w.Width != size || w.Height != size || w.Players != 64 || w.Name != fmt.Sprintf("valley-%d", size) {
			t.Errorf("valley-%d: unexpected %+v", size, w)
		}
	}
}

func TestSummaryOfOneToOneHundredMicroseconds(t *testing.T) {
	samples := make([]int64, 100)
	for i := range samples {
		samples[i] = int64(i+1) * 1000
	}
	s, ok := Summarize(samples)
	if !ok {
		t.Fatal("no summary")
	}
	want := Stats{Count: 100, MeanUS: 50.5, MedianUS: 50.5, P95US: 95, P99US: 99, MinUS: 1, MaxUS: 100}
	if s != want {
		t.Errorf("got %+v, want %+v", s, want)
	}
}

func TestTheSummaryDoesNotDependOnTheOrderOfTheSamples(t *testing.T) {
	s, _ := Summarize([]int64{3000, 1000, 2000})
	if s.MedianUS != 2 || s.MeanUS != 2 || s.P95US != 3 || s.P99US != 3 {
		t.Errorf("got %+v", s)
	}
}

func TestASingleSampleIsEveryPercentileAndNoSamplesHaveNoSummary(t *testing.T) {
	s, _ := Summarize([]int64{2500})
	if s.MedianUS != 2.5 || s.P95US != 2.5 || s.P99US != 2.5 || s.MeanUS != 2.5 {
		t.Errorf("got %+v", s)
	}
	if _, ok := Summarize(nil); ok {
		t.Error("an empty series has no summary")
	}
}

func TestSummarizingDoesNotReorderTheCallersSlice(t *testing.T) {
	samples := []int64{3, 1, 2}
	Summarize(samples)
	if samples[0] != 3 || samples[1] != 1 || samples[2] != 2 {
		t.Errorf("the slice was reordered: %v", samples)
	}
}

func TestTheCatalogIsTheRealOneWithTheChangesOfTheWorkload(t *testing.T) {
	w := small(t)
	base, err := LoadCatalog(root, w)
	if err != nil {
		t.Fatal(err)
	}
	if base.Tank.Capacity != 10 || base.Fire.IgnitionChancePPM != 1250 {
		t.Fatalf("not the real catalog: %+v", base)
	}
	w.CatalogSet = []CatalogChange{{Path: "rules.tank.capacity_liters", Value: 1_000_000_000_000}}
	changed, err := LoadCatalog(root, w)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Tank.Capacity != 1_000_000_000_000 || changed.Fire.IgnitionChancePPM != 1250 {
		t.Errorf("unexpected catalog %+v", changed)
	}
	w.CatalogSet = []CatalogChange{{Path: "rules.tank.no_such_figure", Value: 1}}
	if _, err := LoadCatalog(root, w); err == nil {
		t.Error("a change to a figure that does not exist was accepted")
	}
}

func TestRunningTheSameWorkloadTwiceGivesTheSameStateAndCounters(t *testing.T) {
	w, err := ParseWorkload([]byte(`{"name": "t", "seed": 7, "width": 16, "height": 16, "players": 4,
		"warmup_ticks": 20, "measured_ticks": 10,
		"drops_per_1000_tiles": 100, "hot_percent": 75,
		"installations_per_1000_tiles": 20,
		"catalog_file": "catalog/catalog.json",
		"catalog_set": {"rules.tank.capacity_liters": 1000000000000}}`))
	if err != nil {
		t.Fatal(err)
	}
	a, err := Run(w, root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Run(w, root)
	if err != nil {
		t.Fatal(err)
	}
	if a.StateFNV1a != b.StateFNV1a || a.Applied != b.Applied || a.Rejected != b.Rejected {
		t.Errorf("runs differ: %+v vs %+v", a, b)
	}
	if a.Applied == 0 || a.Duplicate != 0 {
		t.Errorf("unexpected outcomes %+v", a)
	}
	if a.Stats.Count != 10 {
		t.Errorf("%d samples, want 10", a.Stats.Count)
	}
}

func TestTheCSVRowHasOneFieldPerHeaderColumn(t *testing.T) {
	w := small(t)
	w.CatalogSet = nil
	row, err := Run(w, root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Count(row.CSV(), ","), strings.Count(CSVHeader, ","); got != want {
		t.Errorf("%d commas in the row, %d in the header", got, want)
	}
	if !strings.HasPrefix(row.CSV(), "go,t,8,8,3,10,5,") {
		t.Errorf("unexpected row %s", row.CSV())
	}
}
