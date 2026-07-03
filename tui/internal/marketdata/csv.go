package marketdata

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hyperagent/hyperagent/internal/metrics"
)

// LoadCSV reads an OHLCV CSV for a coin/timeframe from dir. It looks for, in
// order, "<COIN>_<tf>.csv" then "<COIN>.csv" (case-insensitively) so a corpus
// can be either per-timeframe or one-file-per-asset. Returns enriched bars
// oldest-first. A missing file is not an error — it returns (nil, nil) so the
// orchestrator can fall through to the next source.
func LoadCSV(dir, coin, tf string, want int) ([]metrics.Bar, error) {
	if dir == "" {
		return nil, nil
	}
	path := findCSV(dir, coin, tf)
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("marketdata: open %s: %w", path, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // tolerate ragged rows
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("marketdata: read %s: %w", path, err)
	}
	if len(records) == 0 {
		return nil, nil
	}

	cols, dataStart := detectColumns(records[0])
	bars := make([]metrics.Bar, 0, len(records))
	for _, rec := range records[dataStart:] {
		b, ok := parseRow(rec, cols, coin, tf)
		if ok {
			bars = append(bars, b)
		}
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].OpenTime.Before(bars[j].OpenTime) })
	enrich(bars)
	if want > 0 && len(bars) > want {
		bars = bars[len(bars)-want:]
	}
	return bars, nil
}

// findCSV resolves the best matching file for coin/tf, case-insensitively.
func findCSV(dir, coin, tf string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	want := []string{
		strings.ToLower(coin + "_" + tf + ".csv"),
		strings.ToLower(coin + "-" + tf + ".csv"),
		strings.ToLower(coin + ".csv"),
	}
	byName := make(map[string]string, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			byName[strings.ToLower(e.Name())] = filepath.Join(dir, e.Name())
		}
	}
	for _, w := range want {
		if p, ok := byName[w]; ok {
			return p
		}
	}
	return ""
}

// colIndex holds the resolved column positions for a CSV.
type colIndex struct {
	t, o, h, l, c, v int
}

// detectColumns inspects the first row. If it looks like a header (non-numeric
// first cell), it maps named columns case-insensitively and data starts at row 1.
// Otherwise it assumes the conventional positional layout time,open,high,low,
// close[,volume] and data starts at row 0.
func detectColumns(first []string) (colIndex, int) {
	positional := colIndex{t: 0, o: 1, h: 2, l: 3, c: 4, v: 5}
	if len(first) == 0 {
		return positional, 0
	}
	if _, err := strconv.ParseFloat(strings.TrimSpace(first[0]), 64); err == nil {
		return positional, 0 // first cell is a number → headerless
	}
	idx := colIndex{t: -1, o: -1, h: -1, l: -1, c: -1, v: -1}
	for i, name := range first {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "time", "timestamp", "date", "datetime", "open_time", "opentime":
			idx.t = i
		case "open", "o":
			idx.o = i
		case "high", "h":
			idx.h = i
		case "low", "l":
			idx.l = i
		case "close", "c", "price":
			idx.c = i
		case "volume", "vol", "v", "basevolume":
			idx.v = i
		}
	}
	if idx.t < 0 || idx.c < 0 { // unrecognizable header → treat as positional
		return positional, 1
	}
	return idx, 1
}

func parseRow(rec []string, cols colIndex, coin, tf string) (metrics.Bar, bool) {
	get := func(i int) (float64, bool) {
		if i < 0 || i >= len(rec) {
			return 0, false
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(rec[i]), 64)
		return f, err == nil
	}
	closePx, ok := get(cols.c)
	if !ok {
		return metrics.Bar{}, false
	}
	open := closePx
	if v, ok := get(cols.o); ok {
		open = v
	}
	high := maxf(open, closePx)
	if v, ok := get(cols.h); ok {
		high = v
	}
	low := minf(open, closePx)
	if v, ok := get(cols.l); ok {
		low = v
	}
	vol, _ := get(cols.v)

	openTime, ok := parseTime(rec, cols.t)
	if !ok {
		return metrics.Bar{}, false
	}
	step := timeframeDuration(tf)
	return metrics.Bar{
		Coin:      coin,
		Timeframe: tf,
		OpenTime:  openTime,
		CloseTime: openTime.Add(step),
		Open:      open,
		High:      high,
		Low:       low,
		Close:     closePx,
		Volume:    vol,
	}, true
}

// parseTime accepts unix seconds, unix millis, RFC3339, or common date layouts.
func parseTime(rec []string, i int) (time.Time, bool) {
	if i < 0 || i >= len(rec) {
		return time.Time{}, false
	}
	s := strings.TrimSpace(rec[i])
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		switch {
		case n > 1e17: // nanoseconds
			return time.Unix(0, n), true
		case n > 1e14: // microseconds
			return time.UnixMicro(n), true
		case n > 1e11: // milliseconds
			return time.UnixMilli(n), true
		default: // seconds
			return time.Unix(n, 0), true
		}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
