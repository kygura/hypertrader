# Offline price corpus

Drop OHLCV CSV files here to warm the rings without any network — the
`[marketdata] csv_dir` path. Warm-up searches, per asset and timeframe:

1. `<COIN>_<tf>.csv`  e.g. `BTC_1h.csv`, `HYPE_4h.csv`
2. `<COIN>-<tf>.csv`
3. `<COIN>.csv`       (one file per asset, any timeframe)

Matching is case-insensitive. Two layouts are accepted:

**Headered** (columns matched by name, any order):

```csv
time,open,high,low,close,volume
1700000000,100,110,90,105,1000
```

**Headerless positional** (`time,open,high,low,close[,volume]`):

```csv
1700000000,100,110,90,105,1000
```

`time` accepts unix seconds/millis/micros/nanos or RFC3339 / `YYYY-MM-DD[ HH:MM:SS]`.
Only `time` and `close` are required; missing OHLC fields are inferred from
`close`. Close-to-close return and range position are computed on load, so a
CSV-warmed series renders with real shape immediately.

This source is tried before CoinGecko, which is tried before Hyperliquid's own
`candleSnapshot` only when HL returns a thin series — so a fully offline run is
deterministic and free.
