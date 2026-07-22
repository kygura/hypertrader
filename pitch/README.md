# Pitch Materials — Hyperion

Index of all pitch, marketing, and investor materials.

---

## Quick Navigation

### Pitch Deck

- **[pitch.html](pitch.html)** — interactive pitch deck (authoritative version, deployed at the site root)
- **[PITCH.md](PITCH.md)** — authored pitch copy (revised 2026-07-22; contact founders before further edits)

### Founder Materials

- **[YC-APPLICATION.md](YC-APPLICATION.md)** — YC batch prep (timeline, checklist, demo plan)

### Investor Materials

- **[investor/README.md](investor/README.md)** — investor materials index and FAQ
- **[investor/FINANCIAL-PLAN.md](investor/FINANCIAL-PLAN.md)** — revenue model, unit economics, 5-year projections

### Supporting Assets

- **[media/](media/)** — GIFs, videos, images (pitch assets)
- **[mock-tui/](mock-tui/)** — TUI mockups

---

## Document Ownership & Status

| Document | Owner | Status | Lock |
|----------|-------|--------|------|
| PITCH.md | Founders | Revised 2026-07-22 | — |
| YC-APPLICATION.md | Founders | Evergreen | — |
| investor/FINANCIAL-PLAN.md | Founders + Finance | Annual review | — |
| investor/README.md | Founders | Current | — |
| pitch.html | (generated from PITCH.md) | Current | — |

---

## Messaging Guidelines

### For All Materials (Pitch, Deck, Web)

**Emphasize:**
1. **Functional prototype** — the core loop is built and tested
2. **Verifiable proof** — append-only journal with byte-exact signatures
3. **Mandate-driven** — traders set a goal; agent executes
4. **Deterministic risk** — compiled gates, not inference
5. **Agent-native** — MCP protocol, standard LLM integration

**Avoid:**
- "AI trading bot" (too generic)
- "We predict markets" (you don't; you manage to a mandate)
- "Unrealized P&L" (focus on realized)
- Overstatement of LLM capability (the LLM is the reasoner, not the sole driver)

---

## Materials by Audience

### For Investors (Angels, VCs)

1. **[PITCH.md](PITCH.md)** — read the core narrative
2. **[investor/FINANCIAL-PLAN.md](investor/FINANCIAL-PLAN.md)** — understand unit economics and TAM
3. **[investor/README.md](investor/README.md)** — FAQ and competitive positioning
4. Live demo (deployed pitch deck + TUI recording)

### For YC

1. **[YC-APPLICATION.md](YC-APPLICATION.md)** — timeline and checklist
2. **[pitch.html](pitch.html)** — deployed landing page, share the live demo URL
3. Founder video (1 min, unlisted YouTube)
4. **[investor/FINANCIAL-PLAN.md](investor/FINANCIAL-PLAN.md)** — prep for "how will you make money"

### For Early Users / Crypto Community

1. **[pitch.html](pitch.html)** — deployed landing page / interactive deck
2. Live demo + journal walkthrough
3. Social: Twitter/Discord with landing page link

### For Enterprise / Funds

1. **[investor/README.md](investor/README.md)** — enterprise value prop
2. **[investor/FINANCIAL-PLAN.md](investor/FINANCIAL-PLAN.md)** — pricing and licensing
3. **[YC-APPLICATION.md](YC-APPLICATION.md)** — traction and team
4. Custom demo (multi-agent fleet, custom risk gates)

---

## Deployment Checklist

Before sharing any materials publicly:

- [ ] Rotate all API keys (OpenAI, Deepseek, Hyperliquid) in `backend/.env`
- [ ] Verify `.env` is gitignored and not in git history
- [ ] Deploy `pitch/` to Vercel (`cd pitch && vercel --prod`)
- [ ] Test deployed URL in incognito browser
- [ ] Verify media (GIFs, videos) loads
- [ ] Add deployed URL to YC application
- [ ] Test MCP registration (`claude mcp add ...`)
- [ ] Record demo (TUI + dashboard)
- [ ] Record founder video (1 min)

---

## Timeline

| By | Do | Owner |
|----|-----|-------|
| Jul 11 | Deploy pitch deck landing page | Founders |
| Jul 15 | Record TUI demo (VHS) + upload | Engineering |
| Jul 18 | Record founder video | Founders |
| Jul 21 | Fill YC application | Founders |
| Jul 24 | Submit YC | Founders |

See **[YC-APPLICATION.md](YC-APPLICATION.md)** for full timeline.

---

## Directory Structure

```
pitch/
├── README.md                    ← You are here
├── PITCH.md                     ← Authored pitch copy
├── YC-APPLICATION.md            ← YC batch prep
├── pitch.html                   ← Interactive deck (deployed at site root)
├── vercel.json                  ← Rewrites "/" to pitch.html
├── investor/
│   ├── README.md               ← Investor materials index
│   └── FINANCIAL-PLAN.md        ← Revenue model & projections
├── media/                       ← Assets (GIFs, videos)
└── mock-tui/                    ← TUI mockups
```

---

## FAQ

### Can I edit PITCH.md?

No. The pitch copy is authored and final. If core messaging needs to change, contact the founders.

### Where should I add a new asset (screenshot, video)?

→ `media/` folder. Update references in `pitch.html` if needed.

### How do I deploy the pitch page?

→ `cd pitch && vercel --prod` (linked to the `hyperion-landing` Vercel project, live at hypertrade.space / pitch-deploy-blue.vercel.app). `vercel.json` rewrites `/` to `/pitch.html` so the site root serves it directly.

### What's the financial model?

→ **[investor/FINANCIAL-PLAN.md](investor/FINANCIAL-PLAN.md)** — SaaS subscription + flow fees + enterprise licensing. LTV:CAC = 500:1.

### Who are the competitors?

→ **[investor/README.md](investor/README.md)** — static bots (3Commas), copilots (ChatGPT), traditional prop firms.

### What's the YC timeline and checklist?

→ **[YC-APPLICATION.md](YC-APPLICATION.md)** — deadline July 27 8pm PT, demo URL, founder video, application form guidance.

---

## External Links

- **YC Application:** https://www.ycombinator.com/apply
- **Hyperliquid Docs:** https://hyperliquid.gitbook.io/
- **Claude API:** https://anthropic.com/api
- **MCP Spec:** https://modelcontextprotocol.io/

---

## Contact

**Email:** nicolascerrato17@gmail.com

---

*Last updated: July 9, 2026*
