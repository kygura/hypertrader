# VHS demo: Hypertrader cockpit on Hyperliquid testnet — single-screen operator view.
# Prereq: backend daemon running on 127.0.0.1:8787 (-testnet), warmed up with bars
# and a live WS connection (health endpoint reports "connected": true).
# Cockpit is one screen, four panels (MANDATE · MARKET PICTURE · EXECUTION ·
# DECISION JOURNAL) plus a bottom chat bar. Keys: `/` opens chat (slash
# commands or free text to the agent), `m` toggles propose/autonomous mode,
# `q` quits. There is no tab/panel cycling in this build.

Output pitch/media/hypertrader-tui.gif

Set FontSize 16
Set Width 1600
Set Height 900
Set Theme "Catppuccin Mocha"
Set TypingSpeed 60ms
Set Padding 12
Set WaitTimeout 120s

Hide
Type "cd /home/athan/projects/hypertrader && ./tui/hyperagent-tui -core-url http://127.0.0.1:8787"
Enter
Show

# Connected: header shows "● CONNECTED", markets/mandate/execution panels live
Wait+Screen /CONNECTED/
Sleep 3s

# Let the market picture, risk envelope, and journal breathe on screen
Sleep 3s

# Flip execution mode live — mode chip + header status react instantly
Type "m"
Sleep 2s
Type "m"
Sleep 2s

# Open the chat bar and run a slash command — re-synthesize tracked markets
Type "/"
Sleep 1s
Type "/scan"
Enter
Sleep 4s

# Finale: ask the agent directly for a written read on a live market
Type "what's your read on BTC right now?"
Enter
Sleep 16s

Escape
Sleep 1s
Hide
Type "q"
