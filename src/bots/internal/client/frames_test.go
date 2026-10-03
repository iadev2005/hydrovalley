package client

import (
	"encoding/json"
	"testing"
)

func TestEncodeFramesMatchPlayProto(t *testing.T) {
	cases := []struct {
		name string
		got  []byte
		want string
	}{
		{"drop", encodeDrop("a1", 14, 9), `{"type":"drop","action_id":"a1","x":14,"y":9}`},
		{"place", encodePlace("a2", "sprinkler", 5, 6), `{"type":"place_installation","action_id":"a2","installation":"sprinkler","x":5,"y":6}`},
		{"upgrade", encodeUpgrade("a3", "bigger_drop"), `{"type":"buy_upgrade","action_id":"a3","upgrade":"bigger_drop"}`},
		{"demo", encodeDemoMode("a4", true), `{"type":"set_demo_mode","action_id":"a4","enabled":true}`},
		{"reset", encodeReset("a5"), `{"type":"reset_valley","action_id":"a5"}`},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("%s: got %s want %s", c.name, c.got, c.want)
		}
	}
}

func TestDecodeStateReadsStringIntegersAndOmittedFields(t *testing.T) {
	raw := `{"type":"state","tick":"12345678901234567","width":2,"height":2,
	"stages":[1,2,3,5],"humidity":["0","10","20","30"],
	"installations":[{"x":1,"y":1,"installation":"sprinkler","owner_id":"o"}],
	"player":{"water":"900","tank_capacity":"1000","leaves":"15","next_sprinkler_price":"10"}}`
	var st State
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		t.Fatal(err)
	}
	if st.Tick != 12345678901234567 || st.Width != 2 || len(st.Stages) != 4 || st.Humidity[3] != 30 {
		t.Fatalf("unexpected state %+v", st)
	}
	if st.Player == nil || st.Player.Leaves != 15 || st.Player.NextSprinklerPrice != 10 || st.Player.NextDripPrice != 0 {
		t.Fatalf("unexpected player %+v", st.Player)
	}
	if st.Paused || len(st.Fires) != 0 {
		t.Fatalf("omitted fields should be zero: %+v", st)
	}
}

func TestU64AcceptsNumbersToo(t *testing.T) {
	var u U64
	if err := json.Unmarshal([]byte(`42`), &u); err != nil || u != 42 {
		t.Fatalf("got %v, %v", u, err)
	}
	if err := json.Unmarshal([]byte(`"-1"`), &u); err == nil {
		t.Fatal("negative must fail")
	}
}
