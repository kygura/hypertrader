# Hyperion

![Hyperion TUI](pitch/media/hypertrader-tui.gif)

Autonomous trading operator on Hyperliquid. Agents state a mandate in plain language — "reach 60/40 ETH–stablecoin, 90 days, max 8% drawdown" — and the system watches, reasons, and executes. Backend daemon ingests markets, runs reasoning loops via LLM agents, executes through hard-coded risk gates. Every decision journaled and inspectable.

## Architecture

**Full loop:** ingest → reason → execute → journal

- **Backend daemon** (:8787) — Market ingestion, position tracking, order execution, risk gates, event bus
- **MCP server** — Exposes Hyperliquid markets and trading as tools. Claude (or any MCP client) reads data and places orders through same risk gates as daemon
- **TUI cockpit** — Operator interface. Real-time feeds, watchlist, position view, order builder
- **Append-only journal** — Every candidate, thesis, and fill recorded. Proof layer for reputation

## Structure

```
backend/        Core daemon. HTTP+WS on :8787. Market data, execution, metrics.
tui/            Cockpit UI (Go + Lipgloss). Live feeds, position tracking, orders.
docs/           Architecture, YC application notes.
pitch/          Landing page, pitch deck, media.
```

## Quick Start

### Backend
```bash
cd backend
cp .env.example .env  # Set HL_AGENT_KEY, HL_ACCOUNT_KEY, etc.
go run src/main.go
```

Server runs on `http://localhost:8787`. MCP server on stdio (configure via Claude Code MCP settings).

### TUI
```bash
cd tui
go run main.go
```

Cockpit connects to backend. Dashboard view updates live.

## Tech Stack

- **Backend:** Go, Echo (HTTP), WebSocket, Bubble Tea (MCP event handling)
- **Frontend:** Go, Lipgloss v2 (TUI rendering)
- **Reasoning:** Claude / OpenAI / Deepseek (via MCP tool calls)
- **Market Data:** Hyperliquid API (REST + WebSocket)
- **Signing:** Custom EIP-712 implementation, Hyperliquid reference vector verified
- **Metrics:** Prometheus-compatible endpoint

## Key Features

- **Mandate-driven interface** — Goal, horizon, risk envelope in one input
- **Deterministic execution** — Orders pass compiled risk gates before wire
- **Verifiable journal** — Append-only decision record, bit-exact EIP-712 sigs
- **Agent-native** — MCP protocol means Claude (or any LLM) can trade
- **Operator override** — Halt or veto at any time
- **Multi-model reasoning** — Pluggable LLM backends

## Development

Current focus: TUI cockpit build. Render helpers and palette done (lipgloss v2). Next: market feed rendering, order form binding, position updates.

## MCP Usage

Register the MCP server in Claude Code / Claude Desktop:

```bash
claude mcp add hypertrader -- ./backend/src/main.go mcp -address 0xYourMasterAccount
```

Then use Claude with trading tools:

- `read_markets` — Get current order books, funding rates, recent trades
- `read_positions` — Account positions, P&L, exposure
- `place_order` — Submit limit/market orders through risk gates
- `cancel_order` — Cancel open orders

All operations go through the same executor and gates. Signing verified against Hyperliquid reference.
