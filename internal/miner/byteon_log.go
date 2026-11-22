package miner

import (
	"context"
	"io"
	"mime"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

const byteonMinerLogPath = "/api/v1/logs/miner"

func readByteonLogClue(ctx context.Context, c *http.Client, base, token string, trace restTrace) antminerLogClue {
	started := time.Now()
	one, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(one, http.MethodGet, base+byteonMinerLogPath, nil)
	if err != nil {
		return antminerLogClue{}
	}
	req.Header.Set("Accept", "text/plain")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Do(req)
	if err != nil {
		return antminerLogClue{}
	}
	defer resp.Body.Close()
	if trace != nil {
		// The device log can contain config or account information.
		trace(byteonMinerLogPath, http.MethodGet, resp.StatusCode, nil, nil, time.Since(started))
	}
	contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusOK || contentType != "text/plain" {
		return antminerLogClue{}
	}
	const maxLogBytes = 256 << 10
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxLogBytes+1))
	if err != nil || len(data) > maxLogBytes {
		return antminerLogClue{}
	}
	return recentByteonLogClue(string(data), time.Now())
}

var byteonLogTime = regexp.MustCompile(`^\[?(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2}))\]?[ \t]+`)

// The REST specification does not guarantee the log is from the current run.
// Only inspect recent entries with a usable timezone and known signatures.
func recentByteonLogClue(data string, now time.Time) antminerLogClue {
	type entry struct {
		at   time.Time
		body string
	}
	var recent []entry
	for raw := range strings.SplitSeq(data, "\n") {
		if len(raw) > 4096 {
			continue
		}
		match := byteonLogTime.FindStringSubmatch(raw)
		if len(match) == 0 {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, match[1])
		if err != nil || at.Before(now.Add(-15*time.Minute)) || at.After(now.Add(time.Minute)) {
			continue
		}
		recent = append(recent, entry{at, raw[len(match[0]):]})
	}
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].at.Before(recent[j].at) })
	var ordered strings.Builder
	for _, line := range recent {
		ordered.WriteString(line.body)
		ordered.WriteByte('\n')
	}
	return lastAntminerLogClue([]byte(ordered.String()))
}
