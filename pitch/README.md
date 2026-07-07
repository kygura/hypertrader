# pitch

YC-application demo and landing-page assets for Hyperion.

## Demo

![Hyperion TUI showcase](media/hypertrader-tui.gif)

The showcase covers the terminal UI (`hyperagent-tui`) connecting to a live daemon, browsing markets, cycling timeframes, viewing the agent's ranked ideas board, triggering a fresh scan, cycling through execution and agent tabs, and generating a written thesis.

## Regenerating the demo

The demo is a reproducible VHS script. Install VHS:

```sh
go install github.com/charmbracelet/vhs@latest
```

Prerequisites: the `hyperagent` daemon must be running on `127.0.0.1:8787` with `-testnet` flag, execution mode set to `autonomous` in `config.toml`, and at least one watchlist scan completed so the verdict panels have data to display.

The tape's `Output` lines are repo-root-relative, so run it from the repo root:

```sh
vhs pitch/media/hypertrader-tui.tape
```

This regenerates `pitch/media/hypertrader-tui.gif`.

## Landing page

`pitch.html` is the static hosted landing page; `PITCH.md` is the source copy it derives from. Both are authored and final — see the project PITCH for product messaging.
