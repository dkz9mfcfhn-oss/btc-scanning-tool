package miner

import (
	"context"
	"strings"
)

// Read stock status endpoints even when a compatible REST adapter already
// provided hashrate. The return flag distinguishes missing fields from failure.
func readAntminerHTTP(ctx context.Context, base string, creds Credentials) (Reading, bool, []string) {
	var r Reading
	var stockTemperature Reading
	failed := false
	var notes []string
	challenge := ""
	// BTC Tools authenticates against config first, then reuses that challenge
	// for read-only status paths. Configuration contents are discarded.
	config, _ := httpJSONWithChallenge(ctx, base, "/cgi-bin/get_miner_conf.cgi", creds, &challenge)
	if m, e := decode(config); e == nil {
		r = merge(r, poolWorkers(m["pools"]))
	}
	read := func(path string) {
		data, err := httpJSONWithChallenge(ctx, base, path, creds, &challenge)
		if err != nil {
			if !strings.Contains(err.Error(), "HTTP 404") && !strings.Contains(err.Error(), "HTTP 405") {
				failed = true
				notes = append(notes, path+": "+diagnosticError(err))
			}
			return
		}
		p, err := ParseAntminer(data)
		if err != nil {
			failed = true
			notes = append(notes, path+": invalid_status_response")
			return
		}
		if path == "/cgi-bin/get_miner_status.cgi" {
			for _, sensor := range p.Temperatures {
				if strings.HasPrefix(sensor.Source, btcTemperaturePrefix) {
					stockTemperature.Temperatures = append(stockTemperature.Temperatures, sensor)
				}
			}
		}
		r = merge(r, p)
	}
	for _, path := range []string{"/cgi-bin/get_miner_status.cgi", "/cgi-bin/stats.cgi"} {
		if ctx.Err() != nil {
			failed = true
			break
		}
		read(path)
		if hasData(r) && r.TempC != nil && hasBoardMeans(r) {
			break
		}
	}
	if needsHashrateDiagnosis(r) || r.AverageTH == nil {
		read("/cgi-bin/summary.cgi")
	}
	if (needsHashrateDiagnosis(r) || r.Accepted == nil) && (hasData(r) || r.StatusKnown) {
		read("/cgi-bin/pools.cgi")
	}
	if (hasData(r) || r.PowerW != nil || r.StatusKnown) && r.Model == "" {
		read("/cgi-bin/miner_type.cgi")
	}
	r = merge(r, stockTemperature)
	return r, failed, notes
}
