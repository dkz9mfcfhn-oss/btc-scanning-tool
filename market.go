package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"
)

type marketQuote struct {
	At         time.Time
	BTCCNY     float64
	Difficulty float64
	Height     int64
}

func (q marketQuote) CNYPerEH() float64 {
	if q.BTCCNY <= 0 || q.Difficulty <= 0 || q.Height <= 0 || math.IsNaN(q.BTCCNY) || math.IsNaN(q.Difficulty) || math.IsInf(q.BTCCNY, 0) || math.IsInf(q.Difficulty, 0) {
		return 0
	}
	// Use the next block's subsidy in satoshis, including halvings.
	subsidy := (uint64(50*100_000_000) >> ((uint64(q.Height) + 1) / 210_000))
	return (float64(subsidy) / 1e8) * (1e18 / (q.Difficulty * (1 << 32))) * q.BTCCNY
}

func (q marketQuote) fresh(now time.Time) bool {
	return !q.At.IsZero() && now.Sub(q.At) <= 30*time.Minute && q.CNYPerEH() > 0
}

var publicMarketClient = &http.Client{Timeout: 4 * time.Second}

func marketJSON(ctx context.Context, client *http.Client, url string, dst any) error {
	return marketRead(ctx, client, url, func(r io.Reader) error { return json.NewDecoder(r).Decode(dst) })
}

func marketRead(ctx context.Context, client *http.Client, url string, decode func(io.Reader) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("行情接口 HTTP %d", resp.StatusCode)
	}
	return decode(io.LimitReader(resp.Body, 64<<10))
}

func fetchDirectBTCCNY(ctx context.Context, client *http.Client) float64 {
	var coinbase struct {
		Data struct{ Amount, Base, Currency string }
	}
	if marketJSON(ctx, client, "https://api.coinbase.com/v2/prices/BTC-CNY/spot", &coinbase) == nil && coinbase.Data.Base == "BTC" && coinbase.Data.Currency == "CNY" {
		if price, err := strconv.ParseFloat(coinbase.Data.Amount, 64); err == nil && validMeasurement(&price) && price > 0 {
			return price
		}
	}
	var gecko struct{ Bitcoin struct{ CNY float64 } }
	if marketJSON(ctx, client, "https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=cny", &gecko) == nil {
		price := gecko.Bitcoin.CNY
		if validMeasurement(&price) && price > 0 {
			return price
		}
	}
	return 0
}

func fetchBinanceBTCEUR(ctx context.Context, client *http.Client) float64 {
	const tickerPath = "/api/v3/ticker/price?symbols=%5B%22BTCUSDT%22%2C%22EURUSDT%22%2C%22BTCEUR%22%5D"
	for _, base := range []string{"https://data-api.binance.vision", "https://api.binance.com"} {
		var tickers []struct{ Symbol, Price string }
		if marketJSON(ctx, client, base+tickerPath, &tickers) != nil {
			continue
		}
		prices := make(map[string]float64, len(tickers))
		for _, ticker := range tickers {
			if price, err := strconv.ParseFloat(ticker.Price, 64); err == nil && price > 0 && validMeasurement(&price) {
				prices[ticker.Symbol] = price
			}
		}
		// EURUSDT is USDT per euro. Dividing preserves the BTC/USDT spot
		// quote and avoids assuming that a USDT is worth exactly one USD.
		if btcUSDT, eurUSDT := prices["BTCUSDT"], prices["EURUSDT"]; btcUSDT > 0 && eurUSDT > 0 {
			price := btcUSDT / eurUSDT
			if price > 0 && validMeasurement(&price) {
				return price
			}
		}
		if price := prices["BTCEUR"]; price > 0 {
			return price
		}
	}
	return 0
}

func currentFXDate(date string, now time.Time) bool {
	day, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return false
	}
	age := now.UTC().Truncate(24 * time.Hour).Sub(day)
	return age >= -24*time.Hour && age <= 7*24*time.Hour
}

func fetchEURCNY(ctx context.Context, client *http.Client, now time.Time) float64 {
	var rate struct {
		Date, Base, Quote string
		Rate              float64
	}
	if marketJSON(ctx, client, "https://api.frankfurter.dev/v2/rate/EUR/CNY", &rate) == nil &&
		rate.Base == "EUR" && rate.Quote == "CNY" && rate.Rate > 0 && validMeasurement(&rate.Rate) && currentFXDate(rate.Date, now) {
		return rate.Rate
	}
	var ecb struct {
		Day struct {
			Date  string `xml:"time,attr"`
			Rates []struct {
				Currency string  `xml:"currency,attr"`
				Rate     float64 `xml:"rate,attr"`
			} `xml:"Cube"`
		} `xml:"Cube>Cube"`
	}
	if marketRead(ctx, client, "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml", func(r io.Reader) error {
		return xml.NewDecoder(r).Decode(&ecb)
	}) == nil && currentFXDate(ecb.Day.Date, now) {
		for _, rate := range ecb.Day.Rates {
			if rate.Currency == "CNY" && rate.Rate > 0 && validMeasurement(&rate.Rate) {
				return rate.Rate
			}
		}
	}
	return 0
}

func fetchMarketPrice(ctx context.Context, client *http.Client) float64 {
	if btcEUR := fetchBinanceBTCEUR(ctx, client); btcEUR > 0 {
		price := btcEUR * fetchEURCNY(ctx, client, time.Now())
		if price > 0 && validMeasurement(&price) {
			return price
		}
	}
	return fetchDirectBTCCNY(ctx, client)
}

func fetchMarket(ctx context.Context) marketQuote {
	prices := make(chan float64, 1)
	go func() { prices <- fetchMarketPrice(ctx, publicMarketClient) }()
	var tip struct {
		Height     int64
		Difficulty float64
	}
	for _, url := range []string{"https://mempool.space/api/blocks", "https://blockstream.info/api/blocks"} {
		var blocks = []struct {
			Height     int64
			Difficulty float64
		}{}
		if marketJSON(ctx, publicMarketClient, url, &blocks) == nil && len(blocks) > 0 && blocks[0].Height > 0 && blocks[0].Difficulty > 0 {
			tip = blocks[0]
			break
		}
	}
	return marketQuote{At: time.Now().UTC(), BTCCNY: <-prices, Difficulty: tip.Difficulty, Height: tip.Height}
}

func (a *App) refreshMarket(ctx context.Context) marketQuote {
	a.marketIO.Lock()
	defer a.marketIO.Unlock()
	now := time.Now()
	a.mu.Lock()
	quote := a.market
	if !now.Before(a.marketNext) && ctx.Err() == nil {
		a.marketNext = now.Add(5 * time.Minute)
		a.mu.Unlock()
		fresh := fetchMarket(ctx)
		a.mu.Lock()
		if fresh.CNYPerEH() > 0 {
			a.market = fresh
			quote = fresh
		} else {
			a.marketNext = now.Add(time.Minute)
		}
	}
	a.mu.Unlock()
	if quote.fresh(now) {
		return quote
	}
	return marketQuote{}
}

func (a *App) maybeRefreshMarket(ctx context.Context, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.demo || a.marketRunning || now.Before(a.marketNext) || ctx.Err() != nil {
		return
	}
	for _, d := range a.devices {
		if !d.Hidden {
			a.marketRunning = true
			a.jobs.Add(1)
			go func() {
				defer a.jobs.Done()
				a.refreshMarket(ctx)
				a.mu.Lock()
				a.marketRunning = false
				a.mu.Unlock()
			}()
			return
		}
	}
}
