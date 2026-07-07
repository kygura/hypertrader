# Hyperion — mock TUI

A proof-of-concept terminal UI illustrating the operator cockpit described in
the pitch (`../PITCH.md`). All data is simulated in-process; there is no
backend, no venue connection, and no signing. It exists to show the shape of
the product loop — ingest → reason → execute → journal — on one screen.

## Panels

- **Mandate** — the stated goal, horizon, and risk envelope, with live
  progress toward the allocation target.
- **Market picture** — the continuous ingest view: price, funding, OI delta,
  CVD across markets.
- **Decision journal** — the append-only log of written judgment: every
  ingest digest, thesis, order, and fill.
- **Execution** — open positions and the compiled risk gates every order
  passes through.

## Run

```sh
go build -o hypertrader-mock . && ./hypertrader-mock
```

Requires a terminal of at least 96×28. Keys: `h` halt / resume the loop,
`q` quit.

Built with Bubble Tea, Bubbles, and Lip Gloss.
