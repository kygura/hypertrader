# VHS demo: Hyperion cockpit on Hyperliquid testnet — single-screen operator view.
#
# A slow, coherent end-to-end walkthrough: connect → read the room → learn the
# command surface → inspect a market → flip execution mode → force a fresh scan
# → ask the agent for a live written read → quit.
#
# Prereq: backend daemon running on 127.0.0.1:8787 (-testnet), warmed with bars
# and a live WS link (GET /api/health reports "connected": true).
#
# Cockpit is one screen, four panels (MANDATE · MARKET PICTURE · EXECUTION ·
# DECISION JOURNAL) plus a bottom chat bar. Keys: `/` opens chat (slash commands
# or free text to the agent), `m` toggles propose/autonomous, `esc` closes chat,
# `q` quits. No tab/panel cycling in this build.

Output pitch/media/hypertrader-tui.mp4
Output pitch/media/hypertrader-tui.gif

Set FontSize 16
Set Width 1600
Set Height 900
Set Theme "Catppuccin Mocha"
Set TypingSpeed 90ms
Set Framerate 30
Set Padding 12
Set WaitTimeout 120s

Hide
Type "cd /home/athan/projects/hypertrader && ./tui/hyperagent-tui -core-url http://127.0.0.1:8787"
Enter
Show

# --- Connect. Header flips to "● CONNECTED"; all four panels go live. ---
Wait+Screen /CONNECTED/
Sleep 5s

# --- Read the room. Let market picture, risk envelope, and the decision ---
# --- journal breathe — live bars and reasoning tiers tick in on their own. ---
Sleep 5s

# --- Open the chat bar and learn the command surface. ---
Type "/"
Sleep 1s
Type "/help"
Enter
Sleep 6s

# --- Inspect the visualized watchlist. ---
Type "/watch"
Enter
Sleep 5s

# --- Re-timeframe a market — ETH to 4h. Its card repaints from 4h bars. ---
Type "/tf 4h ETH"
Enter
Sleep 5s

# --- Close chat, back to the full decision journal. ---
Escape
Sleep 3s

# --- Flip execution mode live. Mode chip + header status react instantly. ---
Type "m"
Sleep 4s
Type "m"
Sleep 4s

# --- Force a fresh synthesis of the tracked markets. ---
Type "/"
Sleep 1s
Type "/scan"
Enter
Sleep 7s

# --- Finale: ask the agent directly for a written read on a live market. ---
Type "what's your read on BTC right now?"
Enter
Sleep 22s

# --- Close the reply pane and exit cleanly. ---
Escape
Sleep 2s
Hide
Type "q"
