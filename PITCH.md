# Hypertrader — an agentic interface & backend for the on-chain economy

**One-liner:** Hypertrader is a trading agent that runs continuously — ingesting markets, reasoning about them in writing, and executing toward long-term financial goals you state in plain language. Built on Hyperliquid.

---

## Thesis: markets became software. Participation is next.

On-chain markets settled the question of access. Anyone, anywhere, can hold a position on a venue that never closes, with custody in their own hands. What hasn't changed is the participant: a person, awake for a third of the market's hours, competing with firms that never sleep.

Agents change the shape of the participant. Not bots — a bot executes one strategy until it's switched off. An agent holds a **mandate**: a goal with a horizon, risk limits, and judgment about when circumstances have changed. The difference between a limit order and a mandate is the difference between using a market and being represented in one.

The next generation of traders won't learn order types, funding rates, or margin math. They'll state outcomes — what they want to hold, over what period, at what risk — and delegate the mechanics. **The onboarding surface of trading is about to become language.**

Hypertrader is that layer: an agentic interface and backend between human intent and the venue.

## Product: one loop, running continuously

1. **Ingest — a continuous read.** Order books, funding, open interest, positions and flow on Hyperliquid, normalized into one live picture of the market, around the clock.
2. **Reason — judgment in writing.** The picture is weighed against your mandate — horizon, targets, risk limits. Every decision is reasoned in writing before it's acted on.
3. **Execute — direct to the venue.** Orders are sized, staged, and placed directly on Hyperliquid through hard-coded risk gates. Results feed back into the picture, and the loop continues.

The loop is inspectable end to end: you can read every decision the agent has made, and you can stop it at any time.

**The interface is a mandate, not an order ticket.** *"Reach a 60/40 ETH–stablecoin split over 90 days. Keep drawdown under 8%. Leverage capped at 2×."* The agent works the mandate — tranching entries, reading funding regimes, staging limit orders instead of taking wide spreads — and writes down why, decision by decision.

## What's built (working today)

The full loop exists and runs as a single Go binary — the founders' own trading desk, in daily use:

- **Live ingest & aggregation** across 10–30 Hyperliquid markets: multi-timeframe bars with perp-native metrics (CVD, basis, funding trajectory, OI delta, liquidation proximity, cross-asset correlation).
- **Model-agnostic reasoning** (Anthropic / OpenAI / Deepseek): timeframe-batched digests in, schema-validated trade candidates with written theses out — never free text.
- **Deterministic execution layer:** every order passes compiled risk gates — max position, max exposure, max concurrency, price sanity vs. live mark, post-stop cooldown, daily-loss kill-switch. No model output bypasses them.
- **Owned signing.** The master key signs exactly one `approveAgent` transaction. The daemon holds only a scoped agent wallet that can trade but **cannot withdraw**. The EIP-712 signing module is ~300 lines we own, verified byte-exact against Hyperliquid's reference vectors — no SDK dependency.
- **Append-only journal:** every candidate, thesis and fill — audit trail, backtest corpus, and the agent's memory in one.
- **MCP server:** any agent (Claude, or anything speaking MCP) can read markets and place orders through the same gates. Every client shares one path to the wire.
- **Terminal UI** — the operator's cockpit for the personal-tool deployment.

This internal desk is the proving ground, not the product. It de-risks the hard parts — signing, gating, continuous reasoning — and generates the journal evidence the product story rests on.

## The product we're raising to build

The **agentic interface**: a hosted web application where a user states a mandate in plain language and reads the agent's work — decision log, position, risk-against-mandate, progress — with one-click scoped-wallet onboarding and the ability to halt at any time. The backend core already exists; the raise productizes it.

## Why now

- Hyperliquid became the dominant on-chain perp venue (billions/day) with a public, signature-gated API — a full exchange with no gatekeeper, no broker, no API-key custodian. It's the first on-chain venue with the performance and liquidity to make continuous, serious execution possible.
- Every major lab shipped tool-calling agents, and MCP standardized the socket, in the same 18 months.
- The curves cross exactly at "agents that trade" — and the missing piece is trustworthy execution and a mandate-level interface, not intelligence.

## Moat

- **The journal.** Verifiable, append-only decision records — the reputation layer autonomous trading will need.
- **One path to the wire.** Web app, MCP clients and the autonomous loop share one executor and one set of compiled gates. Auditable by construction.
- **Owned signing.** The dangerous layer is code we own and test byte-exact against reference vectors, not an inherited SDK.
- **Mandate-native design.** Competitors ship bots (static strategies) or copilots (chat over charts). The mandate — goal, horizon, risk envelope, written judgment — is the primitive everything here is built around.

## Market

- **Wedge:** on-chain traders who want representation, not another terminal — starting with Hyperliquid's prosumer base.
- **Expansion:** the coming population of trading agents needs an execution layer — scoped signing, per-mandate risk envelopes, verifiable track records, multi-venue routing.
- **Model:** subscription for the hosted agent; bps on autonomously executed flow; enterprise licenses for funds running agent fleets.

## Roadmap

| Horizon | Deliverable |
|---|---|
| Now | Full loop live as internal desk (ingest → reason → execute → journal), MCP interface shipped |
| 6 mo | Hosted agentic interface: mandates in plain language, decision log, one-click scoped-wallet onboarding, halt-anytime |
| 12–18 mo | Execution layer for agents: scoped signing as a service, mandate reputation on verifiable journals, multi-venue |

## The ask

We're pre-launch, raising a pre-seed to (1) ship the hosted agentic interface, (2) harden the scoped-signing service, (3) run supervised live capital to build the public journal that proves the loop.

**founders@hypertrader.xyz** · Built on Hyperliquid
