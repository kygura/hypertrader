package reasoner

import "testing"

func TestParseVerdictsArray(t *testing.T) {
	raw := `Here is my analysis:
[
  {"asset":"HYPE","timeframe":"1h","action":"open_short","size_usd":2500,
   "entry":{"type":"limit","price":41.2},"stop":43.1,"take_profit":37.5,
   "thesis":"lower-high into resistance","confidence":0.7,"requires_confirmation":true},
  {"asset":"ETH","timeframe":"1h","action":"hold","confidence":0.4,"thesis":"chop"}
]
Done.`
	vs, err := ParseVerdicts(raw, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 {
		t.Fatalf("want 2 verdicts, got %d", len(vs))
	}
	if vs[0].Asset != "HYPE" || vs[0].Action != ActionOpenShort {
		t.Fatalf("verdict 0 wrong: %+v", vs[0])
	}
	if vs[0].Provider != "test" {
		t.Fatalf("provider not stamped")
	}
}

func TestParseVerdictsDiscardsInvalid(t *testing.T) {
	// First has invalid action, second has bad confidence; both dropped.
	raw := `[
	  {"asset":"X","action":"teleport","confidence":0.5},
	  {"asset":"Y","action":"hold","confidence":5},
	  {"asset":"Z","action":"hold","confidence":0.5,"thesis":"ok"}
	]`
	vs, err := ParseVerdicts(raw, "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 || vs[0].Asset != "Z" {
		t.Fatalf("want only Z to survive, got %+v", vs)
	}
}

func TestParseVerdictsSingleObject(t *testing.T) {
	raw := `{"asset":"BTC","action":"open_long","size_usd":1000,"confidence":0.6,"thesis":"breakout"}`
	vs, err := ParseVerdicts(raw, "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 || vs[0].Asset != "BTC" {
		t.Fatalf("want single BTC verdict, got %+v", vs)
	}
}

func TestExtractBracketedNested(t *testing.T) {
	in := `prefix [{"a":[1,2],"s":"]not-end["}] suffix`
	got := extractJSONArray(in)
	want := `[{"a":[1,2],"s":"]not-end["}]`
	if got != want {
		t.Fatalf("nested extraction failed:\n got %q\nwant %q", got, want)
	}
}

func TestVerdictValidate(t *testing.T) {
	good := Verdict{Asset: "ETH", Action: ActionHold, Confidence: 0.5}
	if err := good.Validate(); err != nil {
		t.Fatalf("good verdict rejected: %v", err)
	}
	bad := Verdict{Asset: "ETH", Action: ActionOpenLong, Confidence: 0.5, SizeUSD: 0}
	if err := bad.Validate(); err == nil {
		t.Fatal("open with zero size should be invalid")
	}
}
