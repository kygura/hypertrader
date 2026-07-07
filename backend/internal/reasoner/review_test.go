package reasoner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hyperagent/hyperagent/internal/bus"
	"github.com/hyperagent/hyperagent/internal/metrics"
)

// reviewDigest builds a representative review digest for prompt tests.
func reviewDigest() metrics.Digest {
	bars := func(n int) []metrics.Bar {
		out := make([]metrics.Bar, n)
		for i := range out {
			out[i] = metrics.Bar{Close: 100 + float64(i), CloseTime: time.Date(2026, 7, 1, i, 0, 0, 0, time.UTC)}
		}
		return out
	}
	return metrics.Digest{
		Coin: "BTC", Timeframe: "4h", Kind: metrics.DigestReview,
		At:      time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC),
		Current: metrics.Bar{Close: 95000},
		Ladder:  map[string][]metrics.Bar{"1h": bars(3), "4h": bars(2)}, // 1d and 1w missing
		Thesis: &metrics.Thesis{
			Coin: "BTC", Direction: "long", Summary: "higher lows",
			Invalidation: 92000, Version: 2,
		},
		RecentJournal: []string{"[07-06 12:00 thesis] thesis updated"},
	}
}

// TestBuildReviewPrompt verifies the review framing: ladder rungs present,
// missing rungs declared (never fabricated), and the live thesis embedded.
func TestBuildReviewPrompt(t *testing.T) {
	got := BuildReviewPrompt([]metrics.Digest{reviewDigest()}, "")
	for _, want := range []string{
		`"asset":"BTC"`,
		`"review_timeframe":"4h"`,
		`"direction":"long"`,
		`"invalidation":92000`,
		`"ladder_missing":["1d","1w"]`,
		"Return a JSON array with one thesis object per asset above.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("review prompt missing %q\n%s", want, got)
		}
	}
}

// TestBuildTriggerPrompt verifies the trigger framing: the deviation rides
// first-class, the HTF summary is closes-only, and a missing thesis renders as
// null rather than being invented.
func TestBuildTriggerPrompt(t *testing.T) {
	d := metrics.Digest{
		Coin: "SOL", Timeframe: "5m", Kind: metrics.DigestTrigger,
		At:        time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC),
		Current:   metrics.Bar{Close: 150},
		Deviation: &metrics.Deviation{Rule: "zscore_return", Magnitude: 3.4, Timeframe: "5m"},
		Ladder: map[string][]metrics.Bar{
			"4h": {{Close: 148}, {Close: 151}},
		}, // 1d missing
	}
	got := BuildTriggerPrompt([]metrics.Digest{d}, "")
	for _, want := range []string{
		`"asset":"SOL"`,
		`"rule":"zscore_return"`,
		`"magnitude":3.4`,
		`"thesis":null`,
		`"4h":[148,151]`,
		`"htf_missing":["1d"]`,
		"Return a JSON array of verdicts, one per asset above.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("trigger prompt missing %q\n%s", want, got)
		}
	}
}

// TestDefaultSystemPromptPerRole pins the role → framing mapping the adapters
// share.
func TestDefaultSystemPromptPerRole(t *testing.T) {
	cases := []struct {
		role Role
		want string
	}{
		{RoleChat, ChatSystemPrompt},
		{RoleReview, ReviewSystemPrompt},
		{RoleTrigger, TriggerSystemPrompt},
		{RoleBatch, SystemPrompt},
	}
	for _, tc := range cases {
		if got := defaultSystemPrompt(tc.role); got != tc.want {
			t.Errorf("defaultSystemPrompt(%s) selected the wrong framing", tc.role)
		}
	}
}

// TestParseThesisReviews verifies the happy path: create/update/invalidate ops
// decode, and an attached entry verdict survives with provider stamping.
func TestParseThesisReviews(t *testing.T) {
	raw := `Here is my review:
[
  {"coin":"BTC","thesis":{"op":"update","direction":"long","summary":"higher lows hold",
   "invalidation":92000,"targets":[105000],"horizon":"weeks","confidence":0.7},
   "verdict":{"asset":"BTC","timeframe":"4h","action":"open_long","size_usd":2000,
    "entry":{"type":"limit","price":94800},"confidence":0.65,"thesis":"pullback entry"}},
  {"coin":"ETH","thesis":{"op":"invalidate"}},
  {"coin":"SOL","thesis":{"op":"create","direction":"neutral","summary":"chop","confidence":0.5}}
]
Done.`
	reviews, discarded, err := ParseThesisReviews(raw, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(discarded) != 0 {
		t.Fatalf("unexpected discards: %v", discarded)
	}
	if len(reviews) != 3 {
		t.Fatalf("want 3 reviews, got %d", len(reviews))
	}
	if reviews[0].Op != "update" || reviews[0].Thesis.Invalidation != 92000 {
		t.Fatalf("review 0 wrong: %+v", reviews[0])
	}
	if reviews[0].Verdict == nil || reviews[0].Verdict.Provider != "test" {
		t.Fatalf("attached verdict lost or unstamped: %+v", reviews[0].Verdict)
	}
	if reviews[1].Coin != "ETH" || reviews[1].Op != "invalidate" {
		t.Fatalf("invalidate op wrong: %+v", reviews[1])
	}
	if reviews[2].Thesis.Direction != "neutral" {
		t.Fatalf("neutral thesis wrong: %+v", reviews[2])
	}
}

// TestParseThesisReviewsDiscardsMalformed verifies validation mirrors
// ParseVerdicts: bad ops, directions, confidences, and attached verdicts are
// discarded (and reported) while valid siblings survive.
func TestParseThesisReviewsDiscardsMalformed(t *testing.T) {
	raw := `[
	  {"coin":"","thesis":{"op":"create","direction":"long","confidence":0.5}},
	  {"coin":"A","thesis":{"op":"teleport","direction":"long","confidence":0.5}},
	  {"coin":"B","thesis":{"op":"create","direction":"sideways","confidence":0.5}},
	  {"coin":"C","thesis":{"op":"create","direction":"short","confidence":5}},
	  {"coin":"D"},
	  {"coin":"E","thesis":{"op":"create","direction":"long","confidence":0.6},
	   "verdict":{"asset":"E","action":"teleport","confidence":0.5}}
	]`
	reviews, discarded, err := ParseThesisReviews(raw, "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 || reviews[0].Coin != "E" {
		t.Fatalf("want only E to survive, got %+v", reviews)
	}
	if reviews[0].Verdict != nil {
		t.Fatal("invalid attached verdict must be dropped")
	}
	if len(discarded) != 6 { // 5 bad items + E's bad verdict
		t.Fatalf("want 6 discard reasons, got %d: %v", len(discarded), discarded)
	}
}

// TestParseThesisReviewsNoJSON verifies pure prose is a hard error (retry-able
// transport-level failure), not a silent empty success.
func TestParseThesisReviewsNoJSON(t *testing.T) {
	if _, _, err := ParseThesisReviews("the market looks fine to me", "p"); err == nil {
		t.Fatal("prose response should error")
	}
}

// fakeThesisStore records the operations the engine applies.
type fakeThesisStore struct {
	upserts     []metrics.Thesis
	invalidated []string
}

func (f *fakeThesisStore) Get(string) (metrics.Thesis, bool) { return metrics.Thesis{}, false }
func (f *fakeThesisStore) Upsert(t metrics.Thesis) (metrics.Thesis, error) {
	f.upserts = append(f.upserts, t)
	t.Version = len(f.upserts)
	return t, nil
}
func (f *fakeThesisStore) Invalidate(coin string) bool {
	f.invalidated = append(f.invalidated, coin)
	return true
}

// roleProvider returns canned per-role responses and records the roles seen.
type roleProvider struct {
	roles []Role
}

func (p *roleProvider) Name() string { return "fake" }
func (p *roleProvider) Complete(_ context.Context, req Request) (Response, error) {
	p.roles = append(p.roles, req.Role)
	switch req.Role {
	case RoleReview:
		return Response{Reviews: []ThesisReview{
			{Coin: "BTC", Op: "update", Thesis: metrics.Thesis{Coin: "BTC", Direction: "long"},
				Verdict: &Verdict{Asset: "BTC", Action: ActionHold, Confidence: 0.5}},
			{Coin: "ETH", Op: "invalidate"},
		}}, nil
	default:
		return Response{Verdicts: []Verdict{
			{Asset: "SOL", Action: ActionHold, Confidence: 0.5},
		}}, nil
	}
}

// TestEngineRoutesKindsAndAppliesReviews verifies the tier plumbing end to
// end inside the engine: review digests reach RoleReview and mutate the thesis
// store; trigger digests reach RoleTrigger and their verdicts come back
// stamped trigger-sourced; review-attached verdicts are review-sourced.
func TestEngineRoutesKindsAndAppliesReviews(t *testing.T) {
	prov := &roleProvider{}
	reg := NewRegistry(map[string]Provider{"fake": prov}, map[string][]string{"fake": {"m"}},
		"fake", "m", "fake", "m")
	var verdicts []Verdict
	e := NewEngine(bus.New(), reg, nil, func(v Verdict) { verdicts = append(verdicts, v) })
	ts := &fakeThesisStore{}
	e.AttachThesisStore(ts, nil)

	e.reason(context.Background(), metrics.DigestReview,
		[]metrics.Digest{{Coin: "BTC", Timeframe: "4h", Kind: metrics.DigestReview}})
	e.reason(context.Background(), metrics.DigestTrigger,
		[]metrics.Digest{{Coin: "SOL", Timeframe: "5m", Kind: metrics.DigestTrigger}})

	if len(prov.roles) != 2 || prov.roles[0] != RoleReview || prov.roles[1] != RoleTrigger {
		t.Fatalf("roles seen = %v, want [review trigger]", prov.roles)
	}
	if len(ts.upserts) != 1 || ts.upserts[0].Coin != "BTC" {
		t.Fatalf("upserts = %+v, want one BTC update", ts.upserts)
	}
	if len(ts.invalidated) != 1 || ts.invalidated[0] != "ETH" {
		t.Fatalf("invalidated = %v, want [ETH]", ts.invalidated)
	}
	if len(verdicts) != 2 {
		t.Fatalf("verdicts = %+v, want 2", verdicts)
	}
	if verdicts[0].Source != metrics.DigestReview {
		t.Fatalf("review-attached verdict source = %q, want review", verdicts[0].Source)
	}
	if verdicts[1].Source != metrics.DigestTrigger {
		t.Fatalf("trigger verdict source = %q, want trigger", verdicts[1].Source)
	}
}
