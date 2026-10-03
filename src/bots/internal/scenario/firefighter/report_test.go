package firefighter

import (
	"encoding/json"
	"strings"
	"testing"
)

func rec(outcome string, rounds int, bonus ...int) FireRecord {
	return FireRecord{X: 1, Y: 2, Outcome: outcome, Rounds: rounds, Drops: rounds * 3, BonusBots: bonus}
}

func TestBuildReportCountsAndPasses(t *testing.T) {
	rep := BuildReport(Measurement{Bots: 3, Fires: []FireRecord{rec(OutcomeExtinguished, 1, 2), rec(OutcomeExtinguished, 1, 0)}})
	if rep.FiresSeen != 2 || rep.Extinguished != 2 || !rep.SingleBonusOK || !rep.Passed() {
		t.Fatalf("unexpected %+v", rep)
	}
}

func TestBuildReportWithoutFiresPasses(t *testing.T) {
	rep := BuildReport(Measurement{Bots: 3})
	if !rep.Passed() || rep.FiresSeen != 0 {
		t.Fatalf("unexpected %+v", rep)
	}
	if !strings.Contains(FormatSummary(rep), "no hubo incendios en la ventana") {
		t.Fatalf("summary must say so:\n%s", FormatSummary(rep))
	}
}

func TestBuildReportFailures(t *testing.T) {
	cases := map[string]FireRecord{
		"burned out after being attacked": rec(OutcomeBurnedOut, 2),
		"still burning at the end":        rec(OutcomeUnresolved, 3),
		"two bonuses":                     rec(OutcomeExtinguished, 1, 0, 1),
	}
	for name, f := range cases {
		if rep := BuildReport(Measurement{Bots: 3, Fires: []FireRecord{f}}); rep.Passed() {
			t.Errorf("%s: must fail: %+v", name, rep)
		}
	}
}

func TestBuildReportDoesNotFailOnWhatIsIndicativeOrNotEvaluated(t *testing.T) {
	// Sin bono visible (heurística) y fuego aparecido al final sin atacar.
	rep := BuildReport(Measurement{Bots: 3, Fires: []FireRecord{rec(OutcomeExtinguished, 1), rec(OutcomePendingAtEnd, 0)}})
	if !rep.Passed() {
		t.Fatalf("must pass: %+v", rep)
	}
	if len(rep.Notes) == 0 {
		t.Fatal("a missing bonus must be noted")
	}
}

func TestBuildReportCountersAreInformational(t *testing.T) {
	rep := BuildReport(Measurement{Bots: 1, Baseline: Counters{Humidity: 100, Accepted: 4}, Final: Counters{Humidity: 175, Accepted: 7}})
	if rep.HumidityContributed != 75 || rep.AcceptedDrops != 3 {
		t.Fatalf("deltas wrong: %+v", rep)
	}
	back := Measurement{Bots: 1, Baseline: Counters{Humidity: 100, Accepted: 4}, Final: Counters{Humidity: 5, Accepted: 1}}
	if r := BuildReport(back); len(r.Notes) == 0 || r.HumidityContributed != 0 {
		t.Fatalf("a counter that went back must be noted: %+v", r)
	}
}

func TestFormatSummaryAndJSON(t *testing.T) {
	rep := BuildReport(Measurement{Bots: 3, Fires: []FireRecord{{X: 4, Y: 5, Outcome: OutcomeExtinguished, Rounds: 1, Drops: 3, Applied: 3, BonusBots: []int{1}}}})
	out := FormatSummary(rep)
	for _, want := range []string{"3 bots", "(4, 5)", "apagado", "indicativo", "Resultado: CUMPLE"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	data, err := MarshalReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		FiresSeen int `json:"fires_seen"`
		Fires     []struct {
			BonusBots []int `json:"bonus_bots"`
		} `json:"fires"`
	}
	if err := json.Unmarshal(data, &back); err != nil || back.FiresSeen != 1 || len(back.Fires[0].BonusBots) != 1 {
		t.Fatalf("unexpected JSON %s (%v)", data, err)
	}
}
