# Financial Plan & Unit Economics

Hyperion's revenue model and path to profitability.

## Reality Check

No proprietary moat here. The EIP-712 signing, the reasoning loop, the risk gates could all be rebuilt by a competitor with enough time. Hyperion ships as a composable server: a long-running backend you either self-host or run on our managed cloud. This plan prices like infrastructure, not like a defensible black box.

## Revenue

Managed Cloud is the primary line: a monthly subscription to run the backend on our infrastructure instead of your own server.

| Tier | Monthly | Agents | Target user |
|---|---|---|---|
| Starter | $49 | 1, shared infra | Retail traders |
| Pro | $199 | Up to 5, dedicated resources | Semi-pro, small funds |
| Enterprise | From $2k | Dedicated deployment, SLA | Trading firms |

Self-hosters can run the same binary for free, so what's actually for sale is convenience: no ops burden, always-on, support. A self-hosted Pro license ($500 to $2,000 a year) unlocks multi-agent orchestration, alerting, and priority support for people who don't want the cloud tier. Enterprise deals ($5k to $20k a month) cover dedicated deployments and custom risk gates for funds.

## Unit Economics

CAC is low and mostly organic: content, community, and a little event spend land around $167 per customer blended, on roughly $20k of spend for 120 users a year.

Because there's no lock-in, churn is realistically high: call it 25% a year, a 2-year average customer life. A Managed Cloud Pro customer is worth about $4,800 over that lifetime; a self-hosted license customer about $2,400. Blended LTV lands near $3,800, for an LTV:CAC around 23:1. Solid, not the inflated 500:1 you'd get by pretending the tech itself is the moat.

Self-hosted customers cost us almost nothing beyond support. Managed Cloud customers cost roughly $25 to $45 a month in hosting and reasoning API spend against $199 of revenue, around 82% margin there and about 85% blended.

## Projections

| Year | Customers | ARR | Operating result |
|---|---|---|---|
| 1 | 60 | ~$67k | -$280k |
| 2 | 150 | ~$216k | -$180k |
| 3 | 400 | ~$600k | ~breakeven |

Years 4 and 5: 800 to 1,200 customers plus a handful of enterprise deals, $1.5M to $3M ARR, 20-30% operating margin. Slower and thinner than a hard-moat SaaS story, which is consistent with higher churn and infrastructure-style pricing.

## Market

Hyperliquid has about 50,000 active traders. If 10-15% will try a self-hosted or managed agent, 500 paying customers at a $100 a month blend gets to roughly $600k ARR near-term. Widen to all on-chain perp venues (200,000+ traders) and the 5-year opportunity is closer to $3.6M ARR. Add 30-50 enterprise deployments at $8k a month average and enterprise alone could add another $3.8M ARR. Conservative total: $5M to $15M by 2030, smaller than a typical SaaS pitch but realistic for an infrastructure product without a hard moat.

## Use of Funds ($500k)

| Item | Amount |
|---|---|
| Founder salary | $100k |
| Infra and hosting | $75k |
| Reasoning and API costs | $25k |
| Community and content | $50k |
| Buffer | $50k |

A 12-month budget, but runway stretches past 18 months since self-hosters absorb their own infra and reasoning costs.

## Risks

The biggest one is the lack of a technical moat: anyone with enough time could rebuild this. The bet is on distribution and being first to run real capital through it in public, not on defensibility of the code. Other risks worth naming: self-hosting eating into Managed Cloud revenue (expected and priced in), regulatory pressure on on-chain trading, LLM API price hikes (mitigated by multi-model support), and solo-founder key-person risk.

## FAQ

Will this be profitable? Plausibly. About 85% gross margin, 23:1 LTV:CAC, breakeven around year three.

What if someone forks it? They can. It's a composable server, not a locked appliance, so the bet is on trust and convenience rather than code exclusivity.

Can an LLM trade profitably? It isn't trying to beat the market. It executes a stated mandate, risk gates cap the downside regardless of reasoning quality, and the journal is the feedback loop.

Competition? Static bots like 3Commas, chat copilots that never execute, and human prop desks. None of them ship as a composable, self-hostable execution layer.

$100M ARR? Unlikely without a moat-driven premium. More realistically a $5M to $15M business by 2030, unless enterprise outperforms or real defensibility, maybe a reputation network effect, shows up later.

---
Prepared July 2026. Reviewed by founder.
