package apiclient

import (
	"testing"
	"time"
)

func TestCachePutBarReplacesOnSameCloseTime(t *testing.T) {
	c := NewCache()
	now := time.Now()

	// First bar
	b1 := Bar{
		Coin:      "BTC",
		Timeframe: "1m",
		CloseTime: now,
		Close:     50000,
	}
	c.PutBar(b1)

	// Second bar with same CloseTime but different Close
	b2 := Bar{
		Coin:      "BTC",
		Timeframe: "1m",
		CloseTime: now,
		Close:     51000,
	}
	c.PutBar(b2)

	// Should have only 1 bar (replaced), not 2
	hist := c.History("BTC", "1m", 10)
	if len(hist) != 1 {
		t.Errorf("expected 1 bar, got %d", len(hist))
	}

	// Should have the second bar's values (replacement)
	if hist[0].Close != 51000 {
		t.Errorf("expected Close=51000, got %f", hist[0].Close)
	}
}

func TestCachePutBarAppendOnDistinctCloseTime(t *testing.T) {
	c := NewCache()
	now := time.Now()

	// First bar
	b1 := Bar{
		Coin:      "ETH",
		Timeframe: "5m",
		CloseTime: now,
		Close:     3000,
	}
	c.PutBar(b1)

	// Second bar with different CloseTime
	b2 := Bar{
		Coin:      "ETH",
		Timeframe: "5m",
		CloseTime: now.Add(5 * time.Minute),
		Close:     3100,
	}
	c.PutBar(b2)

	// Third bar
	b3 := Bar{
		Coin:      "ETH",
		Timeframe: "5m",
		CloseTime: now.Add(10 * time.Minute),
		Close:     3200,
	}
	c.PutBar(b3)

	// Should have 3 bars
	hist := c.History("ETH", "5m", 10)
	if len(hist) != 3 {
		t.Errorf("expected 3 bars, got %d", len(hist))
	}

	// LatestBar should return the most recent
	latest, ok := c.LatestBar("ETH", "5m")
	if !ok {
		t.Errorf("expected LatestBar to return true")
	}
	if latest.Close != 3200 {
		t.Errorf("expected latest Close=3200, got %f", latest.Close)
	}

	// Check order: oldest-first
	if hist[0].Close != 3000 || hist[1].Close != 3100 || hist[2].Close != 3200 {
		t.Errorf("expected bars oldest-first [3000, 3100, 3200], got [%f, %f, %f]", hist[0].Close, hist[1].Close, hist[2].Close)
	}
}

func TestCacheHistoryLastN(t *testing.T) {
	c := NewCache()
	now := time.Now()

	// Add 5 bars
	for i := 0; i < 5; i++ {
		b := Bar{
			Coin:      "ADA",
			Timeframe: "1h",
			CloseTime: now.Add(time.Duration(i) * time.Hour),
			Close:     float64(1000 + i*100),
		}
		c.PutBar(b)
	}

	// History(coin, tf, 2) should return last 2 bars
	hist := c.History("ADA", "1h", 2)
	if len(hist) != 2 {
		t.Errorf("expected 2 bars, got %d", len(hist))
	}

	// Should be oldest-first: bars 3 and 4 (Close values 1300, 1400)
	if hist[0].Close != 1300 || hist[1].Close != 1400 {
		t.Errorf("expected last 2 bars [1300, 1400], got [%f, %f]", hist[0].Close, hist[1].Close)
	}
}

func TestCacheApplyMarketsUpdatesAndPreserves(t *testing.T) {
	c := NewCache()

	// Seed initial data for BTC
	c.ApplyMarkets([]MarketEntry{
		{
			Coin: "BTC",
			Mid:  50000,
			AssetCtx: AssetCtx{
				Coin:      "BTC",
				MarkPrice: 50000,
			},
			Position: Position{
				Coin: "BTC",
				Size: 1.0,
			},
		},
	})

	// Verify initial state
	mid := c.Mid("BTC")
	if mid != 50000 {
		t.Errorf("expected BTC Mid=50000, got %f", mid)
	}

	pos := c.Position("BTC")
	if pos.Size != 1.0 {
		t.Errorf("expected BTC Position.Size=1.0, got %f", pos.Size)
	}

	ctx, ok := c.AssetCtx("BTC")
	if !ok {
		t.Errorf("expected AssetCtx for BTC to exist")
	}
	if ctx.MarkPrice != 50000 {
		t.Errorf("expected BTC AssetCtx.MarkPrice=50000, got %f", ctx.MarkPrice)
	}

	// Apply new markets: update BTC, add ETH
	c.ApplyMarkets([]MarketEntry{
		{
			Coin: "BTC",
			Mid:  51000,
			AssetCtx: AssetCtx{
				Coin:      "BTC",
				MarkPrice: 51000,
			},
			Position: Position{
				Coin: "BTC",
				Size: 2.0,
			},
		},
		{
			Coin: "ETH",
			Mid:  3000,
			AssetCtx: AssetCtx{
				Coin:      "ETH",
				MarkPrice: 3000,
			},
			Position: Position{
				Coin: "ETH",
				Size: 0.5,
			},
		},
	})

	// BTC should be updated
	mid = c.Mid("BTC")
	if mid != 51000 {
		t.Errorf("expected BTC Mid=51000 after update, got %f", mid)
	}

	pos = c.Position("BTC")
	if pos.Size != 2.0 {
		t.Errorf("expected BTC Position.Size=2.0 after update, got %f", pos.Size)
	}

	// ETH should be added
	mid = c.Mid("ETH")
	if mid != 3000 {
		t.Errorf("expected ETH Mid=3000, got %f", mid)
	}

	pos = c.Position("ETH")
	if pos.Size != 0.5 {
		t.Errorf("expected ETH Position.Size=0.5, got %f", pos.Size)
	}

	// Apply markets with SOL (3rd coin), update BTC again, omit ETH entirely
	solCtx := AssetCtx{
		Coin:      "SOL",
		MarkPrice: 140,
	}
	solPos := Position{
		Coin: "SOL",
		Size: 10.0,
	}
	c.ApplyMarkets([]MarketEntry{
		{
			Coin: "BTC",
			Mid:  52000,
			AssetCtx: AssetCtx{
				Coin:      "BTC",
				MarkPrice: 52000,
			},
			Position: Position{
				Coin: "BTC",
				Size: 3.0,
			},
		},
		{
			Coin:     "SOL",
			Mid:      140,
			AssetCtx: solCtx,
			Position: solPos,
		},
	})

	// Verify BTC was updated again
	mid = c.Mid("BTC")
	if mid != 52000 {
		t.Errorf("expected BTC Mid=52000 after second update, got %f", mid)
	}

	pos = c.Position("BTC")
	if pos.Size != 3.0 {
		t.Errorf("expected BTC Position.Size=3.0 after second update, got %f", pos.Size)
	}

	// Verify SOL was added
	mid = c.Mid("SOL")
	if mid != 140 {
		t.Errorf("expected SOL Mid=140, got %f", mid)
	}

	pos = c.Position("SOL")
	if pos.Size != 10.0 {
		t.Errorf("expected SOL Position.Size=10.0, got %f", pos.Size)
	}

	ctx, ok = c.AssetCtx("SOL")
	if !ok {
		t.Errorf("expected AssetCtx for SOL to exist")
	}
	if ctx.MarkPrice != 140 {
		t.Errorf("expected SOL AssetCtx.MarkPrice=140, got %f", ctx.MarkPrice)
	}

	// Apply markets again with only BTC, omitting both ETH and SOL entirely
	c.ApplyMarkets([]MarketEntry{
		{
			Coin: "BTC",
			Mid:  53000,
			AssetCtx: AssetCtx{
				Coin:      "BTC",
				MarkPrice: 53000,
			},
			Position: Position{
				Coin: "BTC",
				Size: 4.0,
			},
		},
	})

	// BTC should be updated
	mid = c.Mid("BTC")
	if mid != 53000 {
		t.Errorf("expected BTC Mid=53000 after third update, got %f", mid)
	}

	pos = c.Position("BTC")
	if pos.Size != 4.0 {
		t.Errorf("expected BTC Position.Size=4.0 after third update, got %f", pos.Size)
	}

	// ETH should still be preserved (unchanged from second ApplyMarkets)
	mid = c.Mid("ETH")
	if mid != 3000 {
		t.Errorf("expected ETH Mid=3000 (preserved), got %f", mid)
	}

	pos = c.Position("ETH")
	if pos.Size != 0.5 {
		t.Errorf("expected ETH Position.Size=0.5 (preserved), got %f", pos.Size)
	}

	ctx, ok = c.AssetCtx("ETH")
	if !ok {
		t.Errorf("expected AssetCtx for ETH to still exist (preserved)")
	}
	if ctx.MarkPrice != 3000 {
		t.Errorf("expected ETH AssetCtx.MarkPrice=3000 (preserved), got %f", ctx.MarkPrice)
	}

	// SOL should still be preserved (unchanged from third ApplyMarkets)
	mid = c.Mid("SOL")
	if mid != 140 {
		t.Errorf("expected SOL Mid=140 (preserved), got %f", mid)
	}

	pos = c.Position("SOL")
	if pos.Size != 10.0 {
		t.Errorf("expected SOL Position.Size=10.0 (preserved), got %f", pos.Size)
	}

	ctx, ok = c.AssetCtx("SOL")
	if !ok {
		t.Errorf("expected AssetCtx for SOL to still exist (preserved)")
	}
	if ctx.MarkPrice != 140 {
		t.Errorf("expected SOL AssetCtx.MarkPrice=140 (preserved), got %f", ctx.MarkPrice)
	}
}

func TestCacheSeedHistoryOverwrites(t *testing.T) {
	c := NewCache()
	now := time.Now()

	// Seed initial history for SOL/1m
	bars1 := []Bar{
		{Coin: "SOL", Timeframe: "1m", CloseTime: now, Close: 100},
		{Coin: "SOL", Timeframe: "1m", CloseTime: now.Add(1 * time.Minute), Close: 101},
	}
	c.SeedHistory("SOL", "1m", bars1)

	hist := c.History("SOL", "1m", 10)
	if len(hist) != 2 {
		t.Errorf("expected 2 bars after initial seed, got %d", len(hist))
	}

	// Seed new history (should overwrite)
	bars2 := []Bar{
		{Coin: "SOL", Timeframe: "1m", CloseTime: now.Add(10 * time.Minute), Close: 200},
		{Coin: "SOL", Timeframe: "1m", CloseTime: now.Add(11 * time.Minute), Close: 201},
		{Coin: "SOL", Timeframe: "1m", CloseTime: now.Add(12 * time.Minute), Close: 202},
	}
	c.SeedHistory("SOL", "1m", bars2)

	hist = c.History("SOL", "1m", 10)
	if len(hist) != 3 {
		t.Errorf("expected 3 bars after second seed (overwrite), got %d", len(hist))
	}

	// Verify it's the second set of bars
	if hist[0].Close != 200 || hist[1].Close != 201 || hist[2].Close != 202 {
		t.Errorf("expected bars [200, 201, 202], got [%f, %f, %f]", hist[0].Close, hist[1].Close, hist[2].Close)
	}
}

func TestCacheLatestBarNotFound(t *testing.T) {
	c := NewCache()

	_, ok := c.LatestBar("NONEXISTENT", "1m")
	if ok {
		t.Errorf("expected LatestBar to return false for nonexistent coin/tf")
	}
}

func TestCacheMidZeroDefault(t *testing.T) {
	c := NewCache()

	mid := c.Mid("NONEXISTENT")
	if mid != 0 {
		t.Errorf("expected Mid to return 0 for nonexistent coin, got %f", mid)
	}
}

func TestCacheAssetCtxNotFound(t *testing.T) {
	c := NewCache()

	_, ok := c.AssetCtx("NONEXISTENT")
	if ok {
		t.Errorf("expected AssetCtx to return false for nonexistent coin")
	}
}

func TestCachePositionZeroDefault(t *testing.T) {
	c := NewCache()

	pos := c.Position("NONEXISTENT")
	if pos.Size != 0 {
		t.Errorf("expected Position to return zero Size for nonexistent coin, got %f", pos.Size)
	}
}

func TestCacheRingSizeLimit(t *testing.T) {
	c := NewCache()
	now := time.Now()

	// Add 600 bars (more than the 512 limit)
	for i := 0; i < 600; i++ {
		b := Bar{
			Coin:      "BTC",
			Timeframe: "1m",
			CloseTime: now.Add(time.Duration(i) * time.Minute),
			Close:     float64(50000 + i),
		}
		c.PutBar(b)
	}

	// Should only keep last 512
	hist := c.History("BTC", "1m", 600)
	if len(hist) != 512 {
		t.Errorf("expected 512 bars (ring size limit), got %d", len(hist))
	}

	// The oldest bar should be bar 88 (600 - 512 = 88)
	if hist[0].Close != float64(50000+88) {
		t.Errorf("expected oldest bar Close=%.0f, got %f", float64(50000+88), hist[0].Close)
	}

	// The newest bar should be bar 599
	if hist[len(hist)-1].Close != float64(50000+599) {
		t.Errorf("expected newest bar Close=%.0f, got %f", float64(50000+599), hist[len(hist)-1].Close)
	}
}

func TestCacheHistoryNegativeOrZero(t *testing.T) {
	c := NewCache()
	now := time.Now()

	// Add 3 bars
	for i := 0; i < 3; i++ {
		c.PutBar(Bar{
			Coin:      "BTC",
			Timeframe: "1m",
			CloseTime: now.Add(time.Duration(i) * time.Minute),
			Close:     float64(50000 + i),
		})
	}

	// History(coin, tf, 0) should return all bars
	hist := c.History("BTC", "1m", 0)
	if len(hist) != 3 {
		t.Errorf("expected 3 bars for n<=0, got %d", len(hist))
	}

	// History(coin, tf, -1) should return all bars
	hist = c.History("BTC", "1m", -1)
	if len(hist) != 3 {
		t.Errorf("expected 3 bars for n<0, got %d", len(hist))
	}

	// History(coin, tf, 100) where 100 > len should return all bars
	hist = c.History("BTC", "1m", 100)
	if len(hist) != 3 {
		t.Errorf("expected 3 bars for n>len, got %d", len(hist))
	}
}

func TestCacheHistoryEmptySeriesReturnsEmpty(t *testing.T) {
	c := NewCache()

	hist := c.History("BTC", "1m", 10)
	if len(hist) != 0 {
		t.Errorf("expected empty slice for nonexistent series, got %d bars", len(hist))
	}
}
