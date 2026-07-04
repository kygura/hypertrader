package apiclient

import (
	"encoding/json"
	"testing"
)

func TestActionValid(t *testing.T) {
	cases := []struct {
		action Action
		want   bool
	}{
		{ActionOpenShort, true},
		{ActionOpenLong, true},
		{ActionClose, true},
		{ActionScale, true},
		{ActionHold, true},
		{ActionAlertOnly, true},
		{Action("bogus"), false},
		{Action(""), false},
	}
	for _, c := range cases {
		if got := c.action.Valid(); got != c.want {
			t.Errorf("Action(%q).Valid() = %v, want %v", c.action, got, c.want)
		}
	}
}

func TestActionIsTrade(t *testing.T) {
	cases := []struct {
		action Action
		want   bool
	}{
		{ActionOpenShort, true},
		{ActionOpenLong, true},
		{ActionClose, true},
		{ActionScale, true},
		{ActionHold, false},
		{ActionAlertOnly, false},
		{Action("bogus"), false},
	}
	for _, c := range cases {
		if got := c.action.IsTrade(); got != c.want {
			t.Errorf("Action(%q).IsTrade() = %v, want %v", c.action, got, c.want)
		}
	}
}

func TestBarIsBullish(t *testing.T) {
	cases := []struct {
		name        string
		open, close float64
		want        bool
	}{
		{"close above open", 100, 105, true},
		{"close equals open", 100, 100, true},
		{"close below open", 100, 95, false},
	}
	for _, c := range cases {
		b := Bar{Open: c.open, Close: c.close}
		if got := b.IsBullish(); got != c.want {
			t.Errorf("%s: IsBullish() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPositionDirection(t *testing.T) {
	cases := []struct {
		name      string
		size      float64
		wantLong  bool
		wantShort bool
		wantFlat  bool
	}{
		{"long", 1.5, true, false, false},
		{"short", -2.0, false, true, false},
		{"flat", 0, false, false, true},
	}
	for _, c := range cases {
		p := Position{Size: c.size}
		if got := p.IsLong(); got != c.wantLong {
			t.Errorf("%s: IsLong() = %v, want %v", c.name, got, c.wantLong)
		}
		if got := p.IsShort(); got != c.wantShort {
			t.Errorf("%s: IsShort() = %v, want %v", c.name, got, c.wantShort)
		}
		if got := p.IsFlat(); got != c.wantFlat {
			t.Errorf("%s: IsFlat() = %v, want %v", c.name, got, c.wantFlat)
		}
	}
}

func TestVerdictJSONRoundTrip(t *testing.T) {
	// JSON with snake_case keys that match backend wire format
	verdictJSON := `{
		"asset":"BTC",
		"timeframe":"1h",
		"action":"open_long",
		"size_usd":500.0,
		"entry":{"type":"limit","price":45000.5},
		"stop":44000.0,
		"take_profit":46000.0,
		"thesis":"BTC is bullish",
		"reading":"High OI, positive funding",
		"confidence":0.85,
		"requires_confirmation":true
	}`

	var v Verdict
	err := json.Unmarshal([]byte(verdictJSON), &v)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify the critical multi-word fields that require explicit JSON tags
	if got := v.SizeUSD; got != 500.0 {
		t.Errorf("SizeUSD = %f, want 500.0", got)
	}
	if got := v.TakeProfit; got != 46000.0 {
		t.Errorf("TakeProfit = %f, want 46000.0", got)
	}
	if got := v.RequiresConfirmation; got != true {
		t.Errorf("RequiresConfirmation = %v, want true", got)
	}

	// Verify single-word fields also decode correctly
	if got := v.Asset; got != "BTC" {
		t.Errorf("Asset = %q, want \"BTC\"", got)
	}
	if got := v.Timeframe; got != "1h" {
		t.Errorf("Timeframe = %q, want \"1h\"", got)
	}
	if got := v.Action; got != ActionOpenLong {
		t.Errorf("Action = %q, want \"open_long\"", got)
	}
	if got := v.Stop; got != 44000.0 {
		t.Errorf("Stop = %f, want 44000.0", got)
	}
	if got := v.Thesis; got != "BTC is bullish" {
		t.Errorf("Thesis = %q, want \"BTC is bullish\"", got)
	}
	if got := v.Reading; got != "High OI, positive funding" {
		t.Errorf("Reading = %q, want \"High OI, positive funding\"", got)
	}
	if got := v.Confidence; got != 0.85 {
		t.Errorf("Confidence = %f, want 0.85", got)
	}

	// Verify Entry decoded correctly (also requires explicit tags for Price)
	if got := v.Entry.Type; got != "limit" {
		t.Errorf("Entry.Type = %q, want \"limit\"", got)
	}
	if got := v.Entry.Price; got != 45000.5 {
		t.Errorf("Entry.Price = %f, want 45000.5", got)
	}

	// Verify At and Provider are NOT populated from JSON (they have json:"-" tags)
	if !v.At.IsZero() {
		t.Errorf("At should not be populated from JSON, got %v", v.At)
	}
	if v.Provider != "" {
		t.Errorf("Provider should not be populated from JSON, got %q", v.Provider)
	}
}
