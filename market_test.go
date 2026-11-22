package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type marketTestTransport func(*http.Request) (*http.Response, error)

func (f marketTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestMarketPricePrefersBinanceSpot(t *testing.T) {
	const (
		path      = "/api/v3/ticker/price?symbols=%5B%22BTCUSDT%22%2C%22EURUSDT%22%2C%22BTCEUR%22%5D"
		spot      = "https://data-api.binance.vision" + path
		spotAlt   = "https://api.binance.com" + path
		fx        = "https://api.frankfurter.dev/v2/rate/EUR/CNY"
		fxAlt     = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml"
		btc       = "https://api.coinbase.com/v2/prices/BTC-CNY/spot"
		btcAlt    = "https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=cny"
		spotQuote = `[{"symbol":"BTCUSDT","price":"60000"},{"symbol":"BTCEUR","price":"50100"},{"symbol":"EURUSDT","price":"1.2"}]`
	)
	today := time.Now().UTC()
	fxQuote := fmt.Sprintf(`{"date":%q,"base":"EUR","quote":"CNY","rate":7.25}`, today.Format(time.DateOnly))
	staleFX := fmt.Sprintf(`{"date":%q,"base":"EUR","quote":"CNY","rate":8}`, today.AddDate(0, 0, -9).Format(time.DateOnly))
	ecbQuote := fmt.Sprintf(`<Envelope xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref"><Cube><Cube time=%q><Cube currency="USD" rate="1.2"/><Cube currency="CNY" rate="7.5"/></Cube></Cube></Envelope>`, today.AddDate(0, 0, -3).Format(time.DateOnly))
	type response struct {
		url, body string
	}
	for _, test := range []struct {
		name      string
		responses []response
		want      float64
	}{
		{"Binance cross and yuan conversion", []response{{spot, spotQuote}, {fx, fxQuote}}, 362500},
		{"alternate Binance and stale currency rate", []response{{spot, ""}, {spotAlt, spotQuote}, {fx, staleFX}, {fxAlt, ecbQuote}}, 375000},
		{"direct BTC EUR when cross is missing", []response{{spot, `[{"symbol":"BTCEUR","price":"48000"},{"symbol":"EURUSDT","price":"NaN"}]`}, {fx, fxQuote}}, 348000},
		{"missing conversion", []response{{spot, spotQuote}, {fx, ""}, {fxAlt, ""}, {btc, `{"data":{"amount":"419500","base":"BTC","currency":"CNY"}}`}}, 419500},
		{"invalid Binance and direct backup", []response{{spot, `[{"symbol":"ETHUSDT","price":"3000"}]`}, {spotAlt, `[{"symbol":"BTCUSDT","price":"NaN"}]`}, {btc, ""}, {btcAlt, `{"bitcoin":{"cny":419000}}`}}, 419000},
		{"no valid price", []response{{spot, ""}, {spotAlt, ""}, {btc, ""}, {btcAlt, ""}}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var n int
			client := &http.Client{Transport: marketTestTransport(func(req *http.Request) (*http.Response, error) {
				if n >= len(test.responses) || req.URL.String() != test.responses[n].url {
					t.Fatalf("unexpected request %d: %s", n, req.URL)
				}
				body := test.responses[n].body
				n++
				status := http.StatusOK
				if body == "" {
					status = http.StatusServiceUnavailable
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			if got := fetchMarketPrice(context.Background(), client); got != test.want || n != len(test.responses) {
				t.Fatalf("got %.2f after %d requests, want %.2f after %d", got, n, test.want, len(test.responses))
			}
		})
	}
}
