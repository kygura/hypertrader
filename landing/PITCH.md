# Hypertrader: an autonomous trading operator, built on Hyperliquid

**One-liner:** An autonomous trading operator, built on Hyperliquid.
Hypertrader runs a trading desk that never sleeps: it ingests markets continuously, reasons about them in writing, and executes toward the financial goals you state in plain language.

---

## Thesis

**Attention, not judgement, is the trading bottleneck.**

On-chain markets solved access. Anyone, anywhere, can hold a position on a venue that never closes, with custody in their own hands.

What they didn't solve is attention. Someone still has to read the order book, funding, news, and flow at three in the morning, and still has to be at the screen when a limit order needs replacing before the market moves past it.

That's the value an agent actually adds, and it isn't intelligence. It's capacity: the ability to ingest order book state, news, and liquidity flows continuously, and hold all of it against a **mandate**, not a bot's fixed strategy, but a goal, a horizon, and risk limits a trader already believes in.

It's the medium a thesis gets expressed through. You say what you want to hold, over what period, at what risk, and the agent does the watching. No more staring at charts. No more hoping a limit order fills before the market leaves you behind.

State the outcome, delegate the mechanics. **The onboarding surface of trading is about to become language**, because the execution surface no longer needs a person babysitting it.

This isn't unique to trading. Travel platforms are shipping agents that search, book, and pay for a trip in one pass, with no human touching the transaction. Software can hold a crypto wallet and move funds without ever passing KYC at a bank, so agents already show up as counterparties on-chain.

Industry estimates already put the agentic economy in the trillions of dollars of annual activity by the end of the decade. Markets are the sharpest edge of it: liquid, quantifiable, and open around the clock.

We think trading is the leading edge of the agentic economy, not a side case of it. Hypertrader is built for that edge first: an autonomous trading operator sitting between a trader's mandate and the venue.

## Product: one loop, running continuously

1. **Ingest.** A continuous read: order books, funding, open interest, positions, and flow on Hyperliquid, normalized into one live picture of the market, around the clock.
2. **Reason.** Judgment in writing. The picture gets weighed against your mandate (horizon, targets, risk limits), and every decision is reasoned in writing before it's acted on.
3. **Execute.** Direct to the venue. Orders are sized, staged, and placed on Hyperliquid through hard-coded risk gates. Results feed back into the picture, and the loop continues.

The loop is inspectable end to end. You can read every decision the agent has made, and you can stop it at any time.

**The interface is a mandate, not an order ticket.** *"Reach a 60/40 ETH–stablecoin split over 90 days. Keep drawdown under 8%. Leverage capped at 2×."* The agent works that mandate: tranching entries, reading funding regimes, staging limit orders instead of taking wide spreads, and writing down why at every step.

## What's built (working today)

The full loop exists and runs as a single Go binary, the founders' own trading desk, in daily use:

- **Live ingest & aggregation** across 10–30 Hyperliquid markets: multi-timeframe bars with perp-native metrics (CVD, basis, funding trajectory, OI delta, liquidation proximity, cross-asset correlation).
- **Model-agnostic reasoning** (Anthropic / OpenAI / Deepseek): timeframe-batched digests in, schema-validated trade candidates with written theses out. Never free text.
- **Deterministic execution layer.** Every order passes compiled risk gates: max position, max exposure, max concurrency, price sanity vs. live mark, post-stop cooldown, daily-loss kill-switch. No model output bypasses them.
- **Owned signing.** The master key signs exactly one `approveAgent` transaction. The daemon holds only a scoped agent wallet that can trade but **cannot withdraw**. The EIP-712 signing module is ~300 lines we own, verified byte-exact against Hyperliquid's reference vectors, with no SDK dependency.
- **Append-only journal.** Every candidate, thesis, and fill in one place: audit trail, backtest corpus, and the agent's memory.
- **MCP server:** any agent (Claude, or anything speaking MCP) can read markets and place orders through the same gates. Every client shares one path to the wire.
- **Terminal UI.** The operator's cockpit for the personal-tool deployment.

This internal desk is the proving ground, not the product. It de-risks the hard parts (signing, gating, continuous reasoning) and generates the journal evidence the product story rests on.

## The product we're raising to build

The **hosted trading operator**: a web application where a user states a mandate in plain language and reads the agent's work (decision log, position, risk against mandate, progress), with one-click scoped-wallet onboarding and the ability to halt at any time. The backend core already exists. The raise productizes it.

## Why now

- Hyperliquid became the dominant on-chain perp venue (billions/day) with a public, signature-gated API: a full exchange with no gatekeeper, no broker, no API-key custodian. It's the first on-chain venue with the performance and liquidity to make continuous, serious execution possible.
- Every major lab shipped tool-calling agents, and MCP standardized the socket, in the same 18 months.
- The curves cross exactly at "agents that trade." What's missing is trustworthy execution and a mandate-level interface, not intelligence.

## Moat

- **The journal.** Verifiable, append-only decision records: the reputation layer autonomous trading will need.
- **One path to the wire.** Web app, MCP clients, and the autonomous loop share one executor and one set of compiled gates. Auditable by construction.
- **Owned signing.** The dangerous layer is code we own and test byte-exact against reference vectors, not an inherited SDK.
- **Mandate-native design.** Competitors ship bots (static strategies) or copilots (chat over charts). Everything here is built around the mandate: goal, horizon, risk envelope, written judgment.

## Market

- **Wedge:** on-chain traders who want representation, not another terminal. Starting with Hyperliquid's prosumer base.
- **Expansion:** the coming population of trading agents needs an execution layer: scoped signing, per-mandate risk envelopes, verifiable track records, multi-venue routing.
- **Model:** subscription for the hosted agent, bps on autonomously executed flow, and enterprise licenses for funds running agent fleets.

### Sizing the opportunity

| | Estimate | What it represents |
|---|---|---|
| **TAM**, agent-mediated economic activity, global | ~$20T/yr by 2030 | The broad shift: agents executing on stated intent across commerce, finance, and services. Midpoint of published estimates. PwC projects $2.6–4.4T in annual GDP contribution from agentic AI by 2030; McKinsey projects ~$13T in additional economic output from AI agents, $3–5T of that in agentic commerce alone. |
| **SAM**, on-chain markets an agent can execute against | ~$3T/yr by 2030 | On-chain spot and derivatives volume, extrapolated from current trajectory. Hyperliquid alone has cleared $4.4T in cumulative perp volume to date and runs at a multi-hundred-billion-dollar monthly clip (DefiLlama); on-chain venues broadly scale with it as agentic participation grows. |
| **SOM**, Hypertrader's near-term wedge | ~$50M/yr addressable | Subscription + bps on flow from Hyperliquid's prosumer trader base stating mandates in the first 12–18 months. A sliver of SAM, sized to what one execution layer can actually capture early. |

*Order-of-magnitude estimates we derived, not third-party forecasts of Hypertrader specifically. Built from published agentic-AI economic-impact research (PwC, McKinsey) and on-chain volume data (DefiLlama).*

## Roadmap

| Horizon | Deliverable |
|---|---|
| Now | Full loop live as internal desk (ingest → reason → execute → journal), MCP interface shipped |
| 6 mo | Hosted trading operator: mandates in plain language, decision log, one-click scoped-wallet onboarding, halt-anytime |
| 12–18 mo | Execution layer for agents: scoped signing as a service, mandate reputation on verifiable journals, multi-venue |

## The ask

We're pre-launch, raising a pre-seed to (1) ship the hosted trading operator, (2) harden the scoped-signing service, (3) run supervised live capital to build the public journal that proves the loop.

**ncerratoanton@gmail.com** 
