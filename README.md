# Hyperion

![Hyperion](pitch/media/hyperion-tui.gif)

Autonomous trading operator on Hyperliquid. Agents state a mandate in plain language — "reach 60/40 ETH–stablecoin, 90 days, max 8% drawdown" — and the system watches, reasons, and executes. Backend daemon ingests markets, runs reasoning loops via LLM agents, executes through hard-coded risk gates. Every decision journaled and inspectable.

## Status

Hyperion is an early, alpha-stage prototype. The backend is functional — it places real orders on Hyperliquid (mainnet or testnet) through a real signer and risk-gated executor — and the TUI is a working but limited operator cockpit. It currently runs as a single-process, single-operator, single-account tool: one instance per config/`.env`, local NDJSON files for persistence, no containerized deployment, no CI, and no multi-tenant or multi-user model, so it is not scalable as-is. There is no billing or account layer, so it is not monetizable today. The web dashboard is a local client SPA you run yourself against your own backend, not a hosted product. The plan is to build a full end-to-end hosted web application that runs the entire pipeline (ingest → reason → execute → journal) as a multi-user product — that work has not started yet.

## Architecture

**Full loop:** ingest → reason → execute → journal

- **Backend daemon** (`:8787`) — Market ingestion, position tracking, order execution, risk gates, event bus. Go, single module (`github.com/hyperagent/hyperagent`).
- **MCP server** — `./hyperagent mcp` exposes Hyperliquid markets and trading as MCP tools over stdio. Claude (or any MCP client) reads data and places orders through the same risk gates as the daemon.
- **TUI cockpit** — Operator terminal UI (separate Go module, `tui/`). Real-time feeds, watchlist, position view, decision journal, chat.
- **Web dashboard** — Browser UI (`dashboard/`, React + Vite). Standalone market/portfolio views plus a live agent console that talks to the daemon over HTTP/WS.
- **Reasoning orchestration** — Harness-first (Claude Code, Codex, `pi` CLIs, run as subprocesses with an env allowlist). Direct API (Claude, OpenAI, Deepseek) fallback. Thesis formation and execution policy are independently configurable roles.
- **Append-only journal** — Every candidate, thesis, and fill recorded as NDJSON. Proof layer for reputation.

## Structure

```
backend/        Core daemon (Go). HTTP+WS on :8787, MCP server, execution, risk gates, journal.
tui/            Cockpit UI (Go + Bubble Tea/Lipgloss v2). Live feeds, position tracking, chat.
dashboard/      Web UI (React 19 + Vite + Tailwind). Market view, portfolio, agent console.
docs/           Architecture, API reference, quickstart, design notes.
pitch/          Landing page, pitch deck, YC application, media.
SPEC.md         Spec for the change currently in flight.
TASKS.md        Task/decision log for that change.
```

## Setup Guide

### Prerequisites

- **Go 1.25.8** — pinned exactly in both `backend/go.mod` and `tui/go.mod`. If your local `go` is a different version, the toolchain auto-downloads 1.25.8 on first build; no action needed, just expect the extra download.
- **Bun** (or Node + npm) — only needed for `dashboard/`. `bun.lock` is committed, so `bun install` is the intended path; `npm install` also works.
- A Hyperliquid account. For real trading you need an **agent wallet** approved against your master account (see below); for read-only/dry-run use you only need a master address.

### 1. Backend daemon

```bash
cd backend
```

There is **no `.env.example`** in this repo — create `backend/.env` yourself (it's gitignored) with whatever of these you need:

```bash
HL_AGENT_KEY=0x...        # agent wallet private key — required to sign/submit orders
HL_MASTER_ADDRESS=0x...   # your master account address — read-only, for equity/positions
HL_MASTER_KEY=0x...       # master account private key — only needed for `approve-agent`, never stored/used by the daemon
ANTHROPIC_API_KEY=...     # only if using the direct-API Anthropic reasoner provider
OPENAI_API_KEY=...        # only if using the direct-API OpenAI reasoner provider
DEEPSEEK_API_KEY=...      # only if using the direct-API Deepseek reasoner provider
```

`backend/.env` is loaded ad hoc on startup (a plain `KEY=VALUE` parser); real environment variables always win over the file.

Approve an agent wallet once, if you haven't:

```bash
./run.sh approve-agent    # uses HL_MASTER_KEY + HL_AGENT_KEY
```

Build and run the daemon:

```bash
./build.sh              # go build -o hyperagent ./src
./hyperagent -testnet   # daemon, config.toml, propose mode by default
```

Other subcommands on the same binary: `./hyperagent mcp` (MCP server, see below), `./hyperagent auth <pi|claude|codex>` (authenticate a harness-based reasoning provider), `./hyperagent doctor` (environment/health check).

Daemon flags: `-config <path>` (default `config.toml`), `-testnet`, `-agent-key`, `-address`. Execution mode (`propose` vs `autonomous`) is set in `config.toml` under `[execution] mode`, not a flag — autonomous mode silently downgrades to propose if there's no valid signer configured.

Server runs on `http://127.0.0.1:8787` (HTTP + WS). Binding to a non-loopback address with no `[api] token` set is a startup error.

Run backend tests: `cd backend && go test ./...` (33 test files, including a byte-exact EIP-712 signing check against the Hyperliquid reference vectors).

### 2. TUI cockpit

```bash
cd tui
go build -o hyperagent-tui ./src
./hyperagent-tui -core-url http://127.0.0.1:8787
```

Requires the backend daemon already running with `[api] enabled = true` — the TUI fails fast with an error if it can't reach `/api/settings` on startup. Flags: `-core-url` (default `http://127.0.0.1:8787`), `-token` (default `$HYPERAGENT_TOKEN`, needed only if the daemon has an API token configured). Minimum terminal size: 96×28.

One screen, five panels — MANDATE · MARKET PICTURE · EXECUTION · THESES · DECISION JOURNAL — plus a chat bar. `/` opens the chat bar and swaps DECISION JOURNAL + THESES for an AGENT reply pane, `m` toggles propose/autonomous mode, `q` (or `ctrl+c`) quits. Slash commands inside chat: `/help`, `/scan`, `/watch`, `/track`, `/tf`, `/mode`, `/clear`.

Run TUI tests: `cd tui && go test ./...`.

### 3. Web dashboard (optional)

```bash
cd dashboard
bun install   # or: npm install
bun run dev   # or: npm run dev — serves on http://localhost:5173
```

Three pages: a standalone market dashboard and portfolio/paper-trading view (no daemon required), plus `/dashboard/agent` — a live intelligence console (status, liquidity regime, theses, propose-mode approvals, decision log, chat) that requires the backend daemon running (`cd backend && ./hyperagent -headless -testnet`). Set `VITE_CORE_URL` in `dashboard/.env.local` to point at a non-default daemon address; it defaults to `http://127.0.0.1:8787`.

Other scripts: `bun run build` (typecheck + Vite build), `bun run lint`, `bun run preview`, `bun run prices` (fetches OHLCV snapshots from Hyperliquid into `public/data/prices.json`).

## Tech Stack

- **Backend:** Go, `internal/api` (HTTP+WS server), `internal/bus` (event bus), `internal/executor` (risk gates + signing), `internal/reasoner` (LLM engine + provider adapters), `internal/journal` (NDJSON audit log), `go-ethereum` (EIP-712 signing)
- **TUI:** Go, Bubble Tea v2 / Lipgloss v2, `gorilla/websocket`
- **Dashboard:** React 19, TypeScript, Vite 8, Tailwind 4, Recharts/D3, `react-grid-layout`
- **Reasoning:** Harness-first (Claude Code, Codex, `pi`, run as subprocesses with an env allowlist); direct API (Claude, OpenAI, Deepseek) fallback
- **Market Data:** Hyperliquid API (REST + WebSocket)
- **Signing:** Custom EIP-712 implementation, verified byte-exact against Hyperliquid reference vectors
- **Metrics:** Prometheus-compatible endpoint

## Key Features

- **Mandate-driven interface** — Goal, horizon, risk envelope in one input
- **Deterministic execution** — Orders pass compiled risk gates before wire
- **Verifiable journal** — Append-only decision record, bit-exact EIP-712 sigs
- **Agent-native** — MCP protocol means Claude (or any LLM) can trade
- **Operator override** — Halt or veto at any time
- **Multi-model reasoning** — Pluggable LLM backends, independently configurable per role (thesis formation vs. execution policy)

## Development

Cockpit build complete: five-panel layout, live feeds, thesis cards, chat-driven slash commands, mode toggle. Per `TASKS.md`, current work in flight adds `hyperagent doctor` and `hyperagent auth <harness>` subcommands for harness-provider setup/health checks. CI (`.github/workflows/ci.yml`) runs backend and TUI Go tests plus a dashboard build on every push and PR to `master`.

## MCP Usage

Register the MCP server in Claude Code / Claude Desktop:

```bash
claude mcp add hyperion -- ./backend/hyperagent mcp -address 0xYourMasterAccount
```

Then use Claude with trading tools:

- `get_markets` — Order books, funding rates, recent trades
- `get_candles` — Historical OHLCV bars
- `get_account` — Account equity and positions
- `get_open_orders` — Currently resting orders
- `place_order` — Submit limit/market orders through risk gates (requires `HL_AGENT_KEY`)
- `cancel_order` — Cancel open orders (requires `HL_AGENT_KEY`)

All operations go through the same `internal/executor` risk gates as the daemon. Signing verified against Hyperliquid reference.

## Further Reading

See `docs/README.md` for the full documentation index, including `QUICKSTART.md` (5-minute setup), `ARCHITECTURE.md` (system diagram and data flow), `API.md` (full HTTP/WS/MCP reference), and `DESIGN.md` (product rationale).
