# YC Application Prep — Hypertrader

**Target batch:** Fall 2026 (October–December, in person, San Francisco)
**On-time deadline:** **July 27, 2026, 8:00 PM PT** — two days later than we assumed (not July 25)
**Decision:** by August 28, 2026. Interviews August–September, video call, same-day decisions.
**Deal:** $500k total — $125k for 7% (post-money SAFE) + $375k uncapped MFN SAFE. Optionally payable in USDC.

Sources: [ycombinator.com/apply](https://www.ycombinator.com/apply) · [howtoapply](https://www.ycombinator.com/howtoapply) · [video](https://www.ycombinator.com/video) · [deal](https://www.ycombinator.com/deal)

---

## 1. Are we in good shape to apply? Yes.

- **Incorporation is not required to apply.** If accepted, YC helps incorporate (US/Canada/Cayman/Singapore).
- **Pre-launch is normal.** ~40% of funded companies per batch are "just an idea." We are well past that: the full loop runs daily as the founders' own desk.
- **Crypto is welcome.** YC runs a Crypto Deals Program (Coinbase, Circle, Solana Foundation partners) and now offers the $500k in USDC.
- **Our strongest asset** is a working product with an unusual proof surface (append-only decision journal, owned EIP-712 signing verified byte-exact). The application should lead with that.
- **Our weakest surface** is everything public-facing: no hosted landing page, no public repo, no demo URL, no traction numbers. All fixable before July 27.

## 2. What must exist before submission

| # | Item | Status | Action |
|---|------|--------|--------|
| 1 | **Founder video (1 min)** | Missing | Record: all founders on camera, no script (bullet points only), no demo footage, no music/editing. Upload as **unlisted YouTube** link. YC states applicants with a video are statistically much more likely to get an interview. |
| 2 | **Demo URL** | Missing | Optional field, but we have real product to show. Plan in §5: VHS-recorded TUI demo (MP4) + dashboard screen capture, embedded on a hosted page. |
| 3 | **Hosted landing page** | `landing/pitch.html` exists, not deployed | Deploy as-is (static host: Vercel/Cloudflare Pages). Do not rewrite the copy — it is authored and final. |
| 4 | **Secrets scrub** | ⚠️ `backend/.env` holds live OpenAI/Deepseek keys in plaintext | Rotate the keys, confirm `.env` is gitignored and absent from git history **before** the repo is pushed anywhere or shown to anyone. |
| 5 | **Dashboard README** | Still Vite template boilerplate | Replace with real build/run instructions if the repo will be shared. |
| 6 | **Public repo?** | No git remote configured | Decide: YC does not require source access; a private repo is fine. Only needed if we want to link code. |

## 3. Application form — draft answers

Answers below are adapted from `landing/PITCH.md` (the authored language). Items marked **[FOUNDERS]** need input only you can give.

### Company

**Describe what your company does (50 chars max):**
> An autonomous trading operator on Hyperliquid
(46 chars)

**What is your company going to make?**
> Hypertrader runs a trading desk that never sleeps. A trader states a mandate in plain language — "reach a 60/40 ETH–stablecoin split over 90 days, drawdown under 8%, leverage capped at 2×" — and the agent does the watching: it ingests order books, funding, and flow on Hyperliquid continuously, reasons about them in writing, and executes through hard-coded risk gates. Every decision is journaled and inspectable; the user can halt at any time. The interface is a mandate, not an order ticket. We're building the hosted web product on top of a backend core that already exists and trades daily.

**Where do you live now / where will the company be based after YC?**
> **[FOUNDERS]**

**If you have a demo, what's the URL?**
> **[TO PREPARE — see §5]**

### Progress

**How far along are you?**
> The full loop — ingest, reason, execute, journal — runs as a single Go binary and is the founders' own trading desk, in daily use. Live ingest across 10–30 Hyperliquid markets with perp-native metrics; model-agnostic reasoning producing schema-validated trade candidates (never free text); a deterministic execution layer where every order passes compiled risk gates; owned ~300-line EIP-712 signing verified byte-exact against Hyperliquid's reference vectors, with a scoped agent wallet that can trade but cannot withdraw; an append-only journal of every candidate, thesis, and fill; an MCP server so any agent can trade through the same gates; and a terminal UI as the operator's cockpit. Pre-launch, pre-revenue: the raise productizes this core into the hosted operator.

**How long have you been working on this? Tech stack?**
> **[FOUNDERS]** for duration. Stack: Go daemon (event-bus architecture), Bubble Tea TUI, React/TypeScript dashboard, HTTP+WS API, Anthropic/OpenAI/Deepseek reasoning backends.

**Do you have revenue / users?**
> No. Pre-launch. The internal desk is the proving ground; the journal it generates is the evidence the product story rests on.

### Idea

**Why did you pick this idea? What domain expertise do you have? How do you know people need this?**
> **[FOUNDERS]** — this must be personal. The thesis to anchor on: attention, not judgment, is the trading bottleneck. On-chain markets solved access; nobody solved having to be at the screen at 3 a.m. when a limit order needs replacing. We built the desk for ourselves first and use it daily — we are user zero.

**Who are your competitors? What do you understand that they don't?**
> Competitors ship bots (static strategies — 3Commas-style grid/DCA tools) or copilots (chat over charts). Everything in Hypertrader is built around the mandate: goal, horizon, risk envelope, written judgment. Three things we hold that they don't: a verifiable append-only journal (the reputation layer autonomous trading will need), one path to the wire (web app, MCP clients, and the loop share one executor and one set of compiled gates), and owned signing rather than an inherited SDK.

**How will you make money?**
> Subscription for the hosted agent, bps on autonomously executed flow, and enterprise licenses for funds running agent fleets. Wedge: Hyperliquid's prosumer base (~$50M/yr near-term addressable). Expansion: the execution layer for trading agents — scoped signing as a service, mandate reputation, multi-venue routing.

**Why now?**
> Hyperliquid became the dominant on-chain perp venue (billions/day) with a public, signature-gated API — the first on-chain venue with the performance and liquidity for continuous, serious execution. In the same 18 months, every major lab shipped tool-calling agents and MCP standardized the socket. The curves cross exactly at "agents that trade." What's missing is trustworthy execution and a mandate-level interface, not intelligence.

### Founders — **[FOUNDERS: all of this section]**

- *Something impressive each founder has built or achieved (1–2 sentences each).* **YC calls this the most important question on the application.** Concrete and verifiable beats titles. If the answer can be "built the live trading desk described above, including the byte-exact EIP-712 signing layer," say so — but each founder needs their own line.
- How long have the founders known each other, how did you meet, have all met in person?
- An interesting project two or more of you built together (preferably outside work/school).
- Who writes the code? (Any non-founder contributions must be disclosed.)
- The "hacked a non-computer system to your advantage" question.
- Something surprising or amusing one of you has discovered.
- Other ideas you considered applying with (YC sometimes funds an alternate idea).

### Legal & money — **[FOUNDERS: all of this section]**

- Any legal entity formed? (No is acceptable.)
- Equity split among founders + titles.
- Investment taken so far, monthly spend, cash in bank, runway.

### Curious

- What convinced you to apply / did someone encourage you / been to YC events / how did you hear about YC. **[FOUNDERS]**

## 4. Founder video — requirements checklist

- [ ] Exactly ~1 minute; **all founders appear and speak**
- [ ] Distributed team? Screen-record a video call — that's explicitly fine
- [ ] No script — bullet points, spontaneous, like talking to a friend
- [ ] Nothing but founders talking: no demo, no slides, no music, no post-production
- [ ] Cover: who you are, what you're building, why
- [ ] Upload **unlisted** (not private) to YouTube; paste the link

## 5. Product demo plan (the image's suggestion)

The demo field wants a **URL**, so the plan is: record the TUI with VHS, capture the dashboard, host both on one page.

**Why VHS fits:** it runs headless in WSL2 (ttyd + ffmpeg, no display server), is fully scripted via a `.tape` file so the recording is reproducible, and emits GIF and MP4 from the same script. Its `Wait /regex/` command handles the nondeterminism of a TUI attached to a live websocket backend — wait on rendered text instead of guessing with sleeps.

**Recording plan:**
1. Run `backend` with `-testnet` (execution mode stays "propose" — no live capital on screen).
2. Record `hyperagent-tui` walking through: markets/funding view → regime board → agent theses with confidence → pending proposals → decision journal navigation. ~30–45 seconds.
3. Output MP4 (for the demo page) + GIF (for README/landing embedding).
4. Complement with a short screen capture of the React dashboard's agent console (live WS push is the wow moment).
5. Host on a single page (the deployed landing page can embed it) → that's the demo URL.

**Starter tape** (`docs/demo.tape`, tune sleeps/waits to real render output):

```tape
Output demo/hypertrader-tui.mp4
Output demo/hypertrader-tui.gif

Set FontSize 20
Set Width 1600
Set Height 900
Set Theme "Catppuccin Frappe"
Set TypingSpeed 75ms
Set Padding 10

Hide
Type "./tui/hyperagent-tui -core-url http://127.0.0.1:8787"
Enter
Show

Wait+Screen /Markets/    # wait for first screen to render
Sleep 3s
Type "2"                  # regime board
Sleep 4s
Type "3"                  # theses
Sleep 4s
Type "4"                  # proposals
Sleep 4s
Type "5"                  # journal
Sleep 3s
Left                      # previous day
Sleep 3s

Hide
Type "q"
Enter
```

*(Backend must already be running on :8787 before `vhs demo.tape` — start it outside the recording. Keybindings above are placeholders; adjust to the TUI's actual tab keys.)*

**Demo caveats:** YC's own advice is not to over-invest in demo production — a real, unpolished product beats a produced video. One focused day on this is the right budget.

## 6. Suggested timeline (today = July 6; deadline July 27)

| By | Do |
|----|-----|
| Jul 8 | Rotate the exposed API keys; scrub `.env` from history. Founders answer all **[FOUNDERS]** items in §3. |
| Jul 11 | Deploy landing page; replace dashboard README. |
| Jul 15 | Record VHS demo + dashboard capture; publish demo page. |
| Jul 18 | Record founder video; upload unlisted. |
| Jul 21 | Fill the application; both founders review answers cold. |
| Jul 24 | Submit — three days of buffer before the July 27 8pm PT cutoff. |
