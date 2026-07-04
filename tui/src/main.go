// Command hyperagent-tui is the standalone terminal client for the hyperagent
// daemon: it holds no backend state of its own, talking exclusively over
// HTTP+WS to a running daemon's unified core API.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/hyperagent/tui/internal/apiclient"
	"github.com/hyperagent/tui/internal/tui"
)

func main() {
	coreURL := flag.String("core-url", "http://127.0.0.1:8787", "hyperagent daemon base URL")
	token := flag.String("token", os.Getenv("HYPERAGENT_TOKEN"), "bearer token, if the daemon requires one")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

	client := apiclient.New(*coreURL, *token)
	cache := apiclient.NewCache()

	settings, err := client.Settings(ctx)
	if err != nil {
		log.Fatalf("hyperagent-tui: could not reach daemon at %s: %v", *coreURL, err)
	}

	// ChatFunc (internal/tui) is (ctx, string, []ChatTurn) (string, error) —
	// only the reply; provider/model come from the cached Settings, not
	// per-call. This wrapper adapts Client.Chat's 4-return signature to that
	// shape.
	chatFn := func(ctx context.Context, msg string, history []apiclient.ChatTurn) (string, error) {
		reply, _, _, err := client.Chat(ctx, msg, history)
		return reply, err
	}

	hasDarkBG := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	model := tui.New(tui.Config{
		Theme:    tui.NewTheme(hasDarkBG),
		Cache:    cache,
		Controls: client,
		Settings: settings, // seeds Visualized/Tracked/Timeframes/Mode/Chat.Provider/Risk from this one fetch
		ChatFn:   chatFn,
		ThesisFn: client.Thesis,
	})

	p := tea.NewProgram(model, tea.WithContext(ctx))
	go tui.PumpWS(ctx, *coreURL, cache, p)
	go tui.PollMarkets(ctx, client, cache, p)

	if _, err := p.Run(); err != nil {
		cancel()
		log.Fatalf("hyperagent-tui: %v", err)
	}
	cancel()
}
