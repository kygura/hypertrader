package reasoner

import (
	"context"
	"slices"
	"testing"
)

// recordProvider is a fake Provider that records the model id it was asked to use,
// so we can assert the registry's bound model actually reaches the request.
type recordProvider struct {
	name      string
	lastModel string
}

func (r *recordProvider) Name() string { return r.name }
func (r *recordProvider) Complete(_ context.Context, req Request) (Response, error) {
	r.lastModel = req.Model
	return Response{Reply: "ok", Model: req.Model}, nil
}

func TestRegistryModelSwitching(t *testing.T) {
	ant := &recordProvider{name: "anthropic"}
	ds := &recordProvider{name: "deepseek"}
	providers := map[string]Provider{"anthropic": ant, "deepseek": ds}
	models := map[string][]string{
		"anthropic": {"claude-opus-4-8", "claude-sonnet-4-6"},
		"deepseek":  {"deepseek-chat", "deepseek-reasoner"},
	}
	reg := NewRegistry(providers, models, "deepseek", "deepseek-chat", "anthropic", "claude-opus-4-8")

	// Initial binding from construction.
	if p, model := reg.Active(RoleChat); p != "anthropic" || model != "claude-opus-4-8" {
		t.Fatalf("chat binding = %s/%s, want anthropic/claude-opus-4-8", p, model)
	}

	// The core fix: switch the MODEL without touching the provider.
	if err := reg.SetModel(RoleChat, "claude-sonnet-4-6"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if p, model := reg.Active(RoleChat); p != "anthropic" || model != "claude-sonnet-4-6" {
		t.Fatalf("after SetModel = %s/%s, want anthropic/claude-sonnet-4-6", p, model)
	}

	// For returns the bound model, and Complete receives it.
	prov, model, ok := reg.For(RoleChat)
	if !ok || model != "claude-sonnet-4-6" {
		t.Fatalf("For(chat) model=%q ok=%v", model, ok)
	}
	if _, err := prov.Complete(context.Background(), Request{Role: RoleChat, Model: model}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if ant.lastModel != "claude-sonnet-4-6" {
		t.Fatalf("provider received model %q, want claude-sonnet-4-6", ant.lastModel)
	}

	// Switching provider resets the model to that provider's default.
	if err := reg.SetProvider(RoleChat, "deepseek"); err != nil {
		t.Fatalf("SetProvider: %v", err)
	}
	if p, model := reg.Active(RoleChat); p != "deepseek" || model != "deepseek-chat" {
		t.Fatalf("after SetProvider = %s/%s, want deepseek/deepseek-chat", p, model)
	}

	// A free-form model id (not in the configured list) is accepted and surfaced in
	// the picker — this is what makes a many-model endpoint addressable.
	if err := reg.SetModel(RoleChat, "deepseek-vision-7b"); err != nil {
		t.Fatalf("SetModel free-form: %v", err)
	}
	if pm := reg.ProviderModels(); !slices.Contains(pm["deepseek"], "deepseek-vision-7b") {
		t.Fatalf("ProviderModels missing free-form model: %v", pm["deepseek"])
	}

	// Unknown provider errors; empty model errors.
	if err := reg.SetProvider(RoleChat, "nope"); err == nil {
		t.Fatal("SetProvider(nope) should error")
	}
	if err := reg.SetModel(RoleChat, ""); err == nil {
		t.Fatal("SetModel(\"\") should error")
	}

	// The batch role is independent of chat changes.
	if p, model := reg.Active(RoleBatch); p != "deepseek" || model != "deepseek-chat" {
		t.Fatalf("batch binding drifted to %s/%s", p, model)
	}
}
