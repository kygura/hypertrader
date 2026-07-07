# VHS demo: Hyperion cockpit on Hyperliquid testnet — single-screen operator view.
#
# Slow, coherent end-to-end walkthrough: connect → read the room → learn the
# command surface → grow the watchlist → re-timeframe a market → flip
# execution mode → force a fresh scan → ask the agent for a live written read
# → quit. Paced for a human watching, not a CI smoke test — every step gets
# room to breathe before the next one starts.
#
# Prereq: backend daemon running on 127.0.0.1:8787 (-testnet), warmed with bars
# and a live WS link (GET /api/health reports "connected": true).
#
# Cockpit is one screen, five panels (MANDATE · MARKET PICTURE · EXECUTION ·
# THESES · DECISION JOURNAL) plus a bottom chat bar. Keys: `/` opens chat
# (slash commands or free text to the agent), `m` toggles propose/autonomous,
# `esc` closes chat, `q` quits. No tab/panel cycling in this build.

Output pitch/media/hyperion-tui.mp4
Output pitch/media/hyperion-tui.gif

Set FontSize 16
Set Width 1600
Set Height 900
Set Theme "Catppuccin Mocha"
Set TypingSpeed 110ms
Set Framerate 30
Set Padding 12
Set WaitTimeout 120s

Hide
Type "cd /home/athan/projects/hypertrader && ./tui/hyperagent-tui -core-url http://127.0.0.1:8787"
Enter
Show

# --- Connect. Header flips to "● CONNECTED"; all four panels go live. ---
Wait+Screen /CONNECTED/
Sleep 6s

# --- Read the room. Let market picture, risk envelope, and the decision ---
# --- journal breathe — live bars and reasoning tiers tick in on their own. ---
Sleep 7s

# --- Open the chat bar and learn the command surface. ---
Type "/"
Sleep 1.5s
Type "/help"
Enter
Sleep 8s

# --- Inspect the visualized watchlist as it stands. ---
Type "/watch"
Enter
Sleep 6s

# --- Grow the watchlist live — add a new coin, subscription goes out. ---
Type "/watch add AVAX"
Enter
Sleep 6s

# --- Re-timeframe a market — ETH to 4h. Its card repaints from 4h bars. ---
Type "/tf 4h ETH"
Enter
Sleep 6s

# --- Close chat, sit with the full decision journal for a moment. ---
Escape
Sleep 4s

# --- Flip execution mode live. Mode chip + header status react instantly. ---
Type "m"
Sleep 5s
Type "m"
Sleep 5s

# --- Force a fresh synthesis of the tracked markets. ---
Type "/"
Sleep 1.5s
Type "/scan"
Enter
Sleep 9s

# --- Finale: ask the agent directly for a written read on a live market. ---
Type "what's your read on BTC right now?"
Enter
Sleep 26s

# --- Close the reply pane and exit cleanly. ---
Escape
Sleep 3s
Hide
Type "q"
