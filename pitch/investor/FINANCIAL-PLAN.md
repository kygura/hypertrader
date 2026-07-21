# Financial Plan & Unit Economics

Hyperion's revenue model and path to profitability.

---

## Honest Starting Point

Hyperion has limited proprietary technical moat. EIP-712 signing, an LLM reasoning loop, and compiled risk gates are all things a well-resourced competitor could rebuild. The product ships as a composable server — a single long-running backend a trader can self-host or run on our managed cloud — not a closed black-box bot. This plan assumes infrastructure-style economics (hosting, support, convenience) rather than a durable-moat premium, and prices accordingly.

## Revenue Streams

### 1. Managed Cloud (primary)

**Model:** Monthly subscription to run the backend on our infrastructure instead of the customer's own server.

| Tier | Monthly | Annual | Agents | Target User |
|------|---------|--------|--------|-------------|
| Starter | $49 | $588 | 1 agent, shared infra | Retail traders, students |
| Pro | $199 | $2,388 | Up to 5 agents, dedicated resources, priority reasoning | Semi-pro, small funds |
| Enterprise | Custom (from $2k/mo) | — | Dedicated deployment, custom risk gates, SLA | Trading firms, funds |

Pricing logic:
- Priced like infrastructure hosting, not like a proprietary black box — a self-hoster can always run the same binary for free.
- - Convenience (no ops burden, always-on, support) is what's for sale, not exclusivity of the technology.
  - - Volume discounts for firms deploying multiple agents.
   
    - ### 2. Self-Hosted Pro License
   
    - **Model:** One-time or annual license for self-hosters who want features beyond the free/open core.
   
    - - Multi-agent orchestration (running several mandates from one control plane)
      - - Premium dashboards, alerting/webhooks
        - - Priority support and onboarding help
          - - Roughly $500-$2,000/year depending on scale
           
            - ### 3. Enterprise Deployments & Support
           
            - **Model:** Custom contracts for funds or trading firms that want a private deployment.
           
            - Example deals:
            - - $5k-$20k/month for a fund's dedicated agent fleet (custom risk gates, private cloud, SLA)
              - - Integration/consulting fees for firms embedding Hyperion into their own infra
               
                - Typical terms: 1-year contracts, dedicated support, custom onboarding.
               
                - ## Unit Economics
               
                - ### Customer Acquisition Cost (CAC)
               
                - Assumption: organic growth + word-of-mouth + content marketing (self-hosted/composable products lean on community, not paid ads).
               
                - | Channel | Spend/Year | Expected Users/Year | CAC |
                - |---------|-----------|---------------------|-----|
                - | Content (docs, blog, build-in-public) | $5k | 40 | $125 |
                - | Community (Discord, open-source contributions) | $5k | 40 | $125 |
                - | Events + speaking | $10k | 20 | $500 |
                - | Partnerships | $0 | 20 | $0 |
                - | **Total** | **$20k** | **120** | **$167** |
               
                - ### Lifetime Value (LTV)
               
                - Because there's no durable moat, we assume higher churn than a typical locked-in SaaS: 25% annual churn (customers can self-host and leave at any time), ~2-year average lifetime.
               
                - Managed Cloud Pro customer:
                - - Pays: $199/month x 12 = $2,388/year
                  - - Lifetime: $2,388 x 2 years = $4,776
                   
                    - Self-hosted Pro license customer:
                    - - Pays: ~$1,200/year average
                      - - Lifetime: $1,200 x 2 years = $2,400
                       
                        - Blended LTV (assume 60% cloud / 40% self-hosted mix): ~$3,830
                       
                        - **LTV:CAC ratio: ~$3,830 / $167 is approximately 23:1** — healthy, but nowhere near the "500:1" a hard-moat product might claim. This is closer to what an open, low-lock-in infrastructure product should expect.
                       
                        - ### Gross Margin
                       
                        - Cost of goods sold (COGS) — this is where the composable-server model actually costs us money, unlike a pure SaaS:
                       
                        - - Managed Cloud hosting (compute + storage per tenant): ~$15-$25/month per instance
                          - - LLM reasoning API costs (Claude/OpenAI/Deepseek): ~$5-$15/user/month depending on activity (self-hosters pay this themselves with their own API key; only managed-cloud customers hit our COGS)
                            - - Support: amortized, ~$5/user/month at scale
                             
                              - Total COGS per managed-cloud user: ~$25-$45/month.
                             
                              - Gross margin on Managed Cloud Pro ($199/month):
                              - - Revenue: $199
                                - - COGS: ~$35
                                  - - Gross profit: ~$164
                                    - - **Margin: ~82%**

                                    Gross margin on self-hosted license (no hosting/reasoning COGS on our side):
                                    - **Margin: ~95%+** (mostly support cost)
                                   
                                    - Blended gross margin: **~85%** (conservative — assumes managed cloud is the majority of revenue, since that's the higher-touch, most requested option).
                                   
                                    - ## Financial Projections (5-Year)
                                   
                                    - Assumptions:
                                    - - Year 1: 60 active customers (mostly Starter/self-hosted, some Pro)
                                      - - Growth: 150% YoY (more conservative than a hard-moat SaaS — low lock-in means renewals have to be earned)
                                        - - Churn: 25% annually
                                          - - CAC: ~$170, improving slightly as community/content compounds
                                           
                                            - ### Year 1 (Launch)
                                            - | Metric | Value |
                                            - |--------|-------|
                                            - | Active customers | 60 |
                                            - | Mix | 40 self-hosted Pro, 20 Managed Cloud |
                                            - | Subscription/license MRR | ~$5,600 |
                                            - | ARR | ~$67,000 |
                                            - | COGS | $15k |
                                            - | R&D + Ops | $150k |
                                            | Sales + Marketing | $20k |
                                 
| **Operating loss** | **-$280k** |
### Year 2
| Metric | Value |
|--------|-------|
| Active customers | 150 |
| MRR | ~$18,000 |
| ARR | ~$216,000 |
| COGS | $40k |
| R&D + Ops | $220k |
| Sales + Marketing | $40k |
| **Operating loss** | **-$180k** |

### Year 3 (Approaching Breakeven)
| Metric | Value |
|--------|-------|
| Active customers | 400 |
| MRR | ~$50,000 |
| ARR | ~$600,000 |
| COGS | $100k |
| R&D + Ops | $350k |
| Sales + Marketing | $80k |
| **Operating profit/loss** | **~breakeven** |

### Year 4-5

Scaling: 800-1,200 customers plus a handful of enterprise deployments, $1.5-$3M ARR, operating margin 20-30% (lower than a hard-moat product, consistent with infrastructure-style pricing and higher churn).

## Addressable Market (SAM)

### Hyperliquid Prosumers (Near-Term TAM)

Population: ~50,000 active traders on Hyperliquid.
Addressable: 10-15% willing to try a self-hosted or managed agent = 5,000-7,500 traders.

Revenue potential: 500 paying customers (Managed Cloud + self-hosted mix), average ~$100/month blended = ~$600k ARR.

### Broader On-Chain Perps (5-Year TAM)

Population: 200,000+ traders across on-chain perp venues.
Addressable: 5% adopt agent-based, self-hostable execution = 10,000 customers.
Revenue potential: 2,000 paying customers, average ~$150/month = ~$3.6M ARR.

### Enterprise Deployments (10-Year TAM)

Population: 500+ crypto hedge funds/prop firms.
Addressable: 30-50 firms want a dedicated, self-hosted or private-cloud deployment.
Revenue potential: 40 contracts x ~$8k/month average = ~$3.8M ARR.

**Total SAM (conservative): $5-15M by 2030** — smaller than a hard-moat SaaS story, but realistic for an infrastructure/composable-server product with real, if modest, lock-in.

## Path to Profitability

| Milestone | Timeline | Status |
|-----------|----------|--------|
| Self-hosted release (binary + docs) | Q3 2026 | In progress |
| Managed Cloud beta | Q4 2026 | Target |
| First paying customers | Q4 2026 | Target |
| $5k MRR | Q1 2027 | Target |
| Approaching breakeven | Year 3 | Projected |

### Unit Economics Sensitivity

If churn rises to 35% (self-hosting makes leaving easy): LTV drops to ~$2,700, LTV:CAC ~16:1 — still workable, thinner.

If CAC rises to $300 (paid acquisition needed): LTV:CAC ~13:1 — still above the 3:1 bar, but margin for error shrinks.

If Managed Cloud hosting costs run higher than expected (~$50/month COGS): blended gross margin drops to ~70% — still healthy for an infra product.

## Use of Funds ($500k Seed)

Requested: $500k (via YC SAFE).

| Item | Amount | Duration |
|------|--------|----------|
| Salary (founder) | $100k | 12 months |
| Infrastructure + hosting (managed cloud build-out) | $75k | 12 months |
| Reasoning/API costs (development + early customers) | $25k | 12 months |
| Community + content (docs, open-source support) | $50k | 12 months |
| Buffer (tax, legal, contingency) | $50k | Ongoing |
| **Total** | **$300k** | **12 months** |

Runway: 18+ months given the lower burn of a composable, largely self-hosted product (customers absorb a lot of their own infra/reasoning cost).

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Low technical moat — easy to replicate | High | Compete on distribution, composability (MCP-native), and being first to run real capital through it publicly, not on defensibility of the code |
| Market adoption slow (TAM smaller than expected) | High | Early user feedback; pivot toward enterprise/private deployments if needed |
| Self-hosting cannibalizes Managed Cloud revenue | Medium | Expected and priced in — self-hosted Pro license is a real revenue line, not just a loss leader |
| Regulatory crackdown on on-chain trading | High | Legal review; diversify to multiple venues |
| Claude/LLM API price hikes | Medium | Multi-model support (OpenAI, Deepseek fallback); self-hosters bring their own key |
| Key person risk (solo founder) | Medium | Document everything; look for a technical co-founder |
| Security breach or signing bug | High | Security review; scoped agent-wallet keys that can trade but never withdraw |

## Key Metrics to Track

**Growth:** active self-hosted instances (best-effort telemetry, opt-in), Managed Cloud signups, churn rate.
**Revenue:** Managed Cloud MRR, license revenue, ARPU, LTV:CAC.
**Product:** median mandate lifetime, orders per user per month, win rate, reasoning confidence trends from the journal.
**Operational:** CAC payback period, gross margin, burn rate, runway.

## Investor FAQs

**Will Hyperion be profitable?**
Plausibly, but the path is closer to infrastructure economics than a hard-moat SaaS: ~85% blended gross margin, LTV:CAC around 23:1, and breakeven targeted around year 3 rather than year 1-2.

**What happens if someone forks it?**
They can — it's a composable server, not a locked appliance. The bet is that distribution, trust from running real capital in public, and convenience (managed cloud, support) matter more than code exclusivity.

**Can an LLM really trade profitably?**
The LLM isn't trying to beat the market — it's executing a stated mandate (a goal, not a return target). Compiled risk gates prevent catastrophic loss regardless of reasoning quality, and the journal gives a feedback loop for improving the reasoning over time.

**What's the competition?**
Static bots (3Commas, grid/DCA tools), chat copilots (advice without execution), and traditional prop desks (humans, not agents). None of them ship as a composable, self-hostable execution layer — that's the differentiation, not a patent.

**Can you scale to $100M ARR?**
Unlikely on this model alone without a moat-driven premium; more realistically this is a $5-15M ARR business by 2030 unless enterprise deployments significantly outperform, or genuine defensibility (e.g., a mandate/reputation network effect) emerges over time.

## Summary

| Metric | Value |
|--------|-------|
| Gross Margin | ~85% |
| LTV:CAC | ~23:1 |
| Breakeven | ~Year 3 |
| Year 3 ARR | ~$600k |
| TAM (5 years) | $5-15M |
| Risk Level | Medium-High (low technical moat, adoption, regulation) |

Hyperion is a smaller, more honest bet than a hard-moat SaaS story: real infrastructure economics, a composable/self-hosted distribution model, and a founder who already runs it on his own capital daily.

---
Prepared: July 2026
Reviewed by: Founder

Prepared: July 2026
Reviewed by: Founder
