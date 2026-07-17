# hyperagent

Autonomous Hyperliquid scanner, reasoning engine and (gated) executor. One Go
binary running as a headless daemon: MCP server for agents, Telegram for
confirmations, HTTP+WS API for any frontend. The terminal UI for the operator
is a separate standalone binary — see [`tui/`](../tui/README.md).
Architecture and design rationale: [plan.md](plan.md).

## Build & run

```sh
./build.sh            # or: go build -o hyperagent ./src
./hyperagent -testnet  # daemon, propose mode, config.toml
```

The daemon has no built-in UI. For an interactive terminal dashboard, build
and run `tui/` against it:

```sh
cd ../tui && go build -o hyperagent-tui ./src
./hyperagent-tui -core-url http://127.0.0.1:8787
```

## Reasoner: providers & role binding

The daemon reasons over market data via language models. Three transport types
are available:

**Harness providers** (recommended): spawn CLI binaries as subprocesses —
`claude`, `pi`, `codex` — with no stored API keys. Requires the CLI already
authenticated locally (e.g., `pi login`, `claude auth`).

**Direct API providers** (fallback): call HTTP APIs directly with keys stored
in `.env`: `openai`, `anthropic`, `deepseek`.

Role binding is **independent**: thesis formation (`RoleReview`) and trade
execution policy (`RoleTrigger`) can use different providers:

```toml
[reasoner]
# Thesis formation (lower frequency, deeper reasoning)
review_provider = "claude-harness"

# Trade execution & verdict gates (high frequency, cost-optimized)
trigger_provider = "pi-harness"
trigger_model = "gpt-5.6-luna"    # pi sub-provider defaults
batch_provider = "pi-harness"      # digest batching (same as trigger)
batch_model = "gpt-5.6-luna"

# Chat (human operator interface; unchanged)
chat_provider = "deepseek"
chat_model = "deepseek-chat"
```

Direct API keys (if using fallback providers) go in `.env`:

```
ANTHROPIC_API_KEY=sk-...
OPENAI_API_KEY=sk-...
DEEPSEEK_API_KEY=sk-...
```

Switch providers at runtime via the HTTP settings endpoint (no restart needed);
the daemon journals provider errors and preserves prior state on failure.

### Checking & managing harness authentication

Two CLI subcommands help verify harness health and authenticate locally:

**`hyperagent doctor`** — reports per-harness health in plain text:

```sh
./hyperagent doctor
```

Output (one block per harness):

```
claude:
  binary: found (/usr/local/bin/claude)
  auth: ok (logged in)
  model: covered by auth status

pi:
  binary: found (/usr/local/bin/pi)
  auth: ok (logged in)
  model: covered by auth status

codex:
  binary: found (/usr/local/bin/codex)
  auth: ok (logged in)
  model: covered by auth status
```

**`hyperagent auth <pi|claude|codex>`** — interactively log into a harness CLI:

```sh
./hyperagent auth claude    # runs `claude auth login` with inherited terminal
./hyperagent auth codex     # runs `codex login` with inherited terminal
./hyperagent auth pi        # pi has no login subcommand; defers to `pi config`
```

The `auth` subcommand preserves HOME and config paths so credentials persist
across sessions, but still excludes exchange signing keys (`HL_AGENT_KEY`,
`HL_MASTER_ADDRESS`, `HL_AGENT_ADDRESS`). OAuth and browser-based flows work
because the terminal is inherited. Note: `pi` has no explicit auth/login
subcommand, so `auth pi` explains this and opens `pi config` instead.

## Wallet setup (execution)

The daemon never sees your master key. Approve a scoped agent wallet once:

```sh
./hyperagent approve-agent -name hyperagent          # mainnet
./hyperagent approve-agent -testnet                  # testnet
```

Reads the master key from `HL_MASTER_KEY` or a hidden prompt, signs one
`approveAgent` action (EIP-712, user-signed scheme), prints a fresh agent
private key. Put it in `.env`:

```
HL_AGENT_KEY=0x...
```

The agent wallet can sign orders but **cannot withdraw**. Approvals expire per
HL policy; re-run with `-agent-key $HL_AGENT_KEY` to renew the same wallet.

Execution stays in `propose` mode (Telegram confirm) until you set
`[execution] mode = "autonomous"` in `config.toml` — and even then every order
passes the hard-coded risk gates in `internal/executor`.

## MCP server

Expose trading as tools to Claude Code / Claude Desktop / any MCP client:

```sh
claude mcp add hyperion -- /path/to/hyperagent mcp -address 0xYOURMASTER
```

| Tool | Needs key? | Does |
|---|---|---|
| `get_markets` | no | mid/mark/funding/OI/premium/volume snapshot |
| `get_candles` | no | OHLCV history (1m…1d) |
| `get_account` | no | positions, account value, withdrawable |
| `get_open_orders` | no | resting orders |
| `place_order` | `HL_AGENT_KEY` | order through the risk gates; rejections name the gate |
| `cancel_order` | `HL_AGENT_KEY` | cancel by oid |

`-address` (or `HL_MASTER_ADDRESS`) gives the gates position visibility; without
it account tools are disabled and exposure gates run blind. All MCP orders are
journaled like any other candidate.

## HTTP API

The same core the MCP server runs on — store, digests, verdicts, journal,
chat, gated execution — is also reachable over HTTP+WS, so any frontend
(the `tui/` client, the web dashboard, a script, `curl`) can attach without
going through a Claude client. Runs whenever `[api] enabled = true`; binds
loopback by default.

```sh
./hyperagent -testnet &
curl -s localhost:8787/api/health
```

| Method & path | Needs key? | Does |
|---|---|---|
| `GET /api/health` | no | connection state, mode, active providers, version |
| `GET /api/markets` | no | latest derived metrics per tracked coin; 404 while the store warms |
| `GET /api/bars/{coin}?tf=1h&n=100` | no | OHLCV + metric history; 404 if empty |
| `GET /api/digests/{coin}` | no | latest frozen digest; 404 if none yet |
| `GET /api/verdicts` | no | latest verdict/thesis per asset, newest-first (`[]` if none) |
| `GET /api/journal?date=YYYY-MM-DD` | no | journal entries for a day (default today UTC) |
| `POST /api/chat` | a chat provider key | `{message, history[]}` → `{reply, provider, model}`; 503 with no engine, 502 on provider error |
| `GET /api/proposals` | no | pending propose-mode candidates |
| `POST /api/proposals/{id}/approve` | `HL_AGENT_KEY` | confirm flow, same risk gates as Telegram; 422 names the gate on rejection |
| `POST /api/proposals/{id}/reject` | no | drop a pending proposal |
| `POST /api/orders` | `HL_AGENT_KEY` | direct order through the risk gates (same semantics as MCP `place_order`); 503 with no signer |
| `DELETE /api/orders/{coin}/{oid}` | `HL_AGENT_KEY` | cancel by oid |
| `GET /api/ws` | no | push stream — frames `{"topic":"bar\|verdict\|journal\|status\|mids","data":{...}}` |

Every error response is `{"error":"..."}`; risk-gate rejections are always 422.

Config (`[api]` in `config.toml`):

```toml
[api]
enabled = true
addr    = "127.0.0.1:8787"
# token = ""   # optional; when set, Bearer auth required (needed off-localhost)
cors_origins = ["http://localhost:5173"]
```

Default bind is loopback-only with no auth — fine for a single-operator
workstation. Setting a non-loopback `addr` with an empty `token` is a startup
error: the daemon refuses to open the execution surface to the network
unauthenticated. Once `token` is set, every `/api/` request needs
`Authorization: Bearer <token>` (and `/api/ws` accepts the same token via
`?token=` query param, since browsers can't set WS request headers).

## Signing guarantees

- L1 actions: msgpack action-hash + phantom-Agent EIP-712 envelope.
- User-signed actions (`approveAgent`): `HyperliquidSignTransaction` domain.
- Both verified in `internal/signing` tests; the user-signed path reproduces the
  reference Python SDK's signature vectors byte-exact
  (`TestUserSignedVectorUsdSend`).

## Testnet smoke test

```sh
./hyperagent approve-agent -testnet        # with a funded testnet master
HL_AGENT_KEY=0x... ./hyperagent mcp -testnet -address 0xMASTER
# then from an MCP client: place_order {"coin":"ETH","action":"open_long","size_usd":15}
```
