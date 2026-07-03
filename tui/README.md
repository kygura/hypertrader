# hyperagent

Autonomous Hyperliquid scanner, reasoning engine and (gated) executor. One Go
binary: TUI for the operator, MCP server for agents, Telegram for confirmations.
Architecture and design rationale: [plan.md](plan.md).

## Build & run

```sh
./build.sh            # or: go build -o hyperagent ./src
./hyperagent          # TUI, propose mode, config.toml
./hyperagent -headless -testnet
```

Provider API keys go in `.env` (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
`DEEPSEEK_API_KEY`).

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
claude mcp add hypertrader -- /path/to/hyperagent mcp -address 0xYOURMASTER
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
