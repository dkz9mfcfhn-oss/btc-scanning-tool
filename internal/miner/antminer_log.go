package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

const antminerLogPath = "/cgi-bin/log.cgi"

// Bitmain documents the plain-text current log as /cgi-bin/log.cgi and
// separates historical logs under /cgi-bin/hlog.cgi. Only the former is read.
// https://docs.bitmain.com/en/antminer/CONTENTS.html#get-miner-log-data
func addAntminerLogEvidence(ctx context.Context, base string, creds Credentials, r Reading, trace restTrace) Reading {
	if ctx.Err() != nil || r.Brand != "Antminer" || !explicitZeroHashrate(r) || strings.HasSuffix(r.Protocol, " REST API") {
		return r
	}
	started := time.Now()
	one, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	data, err := httpGetWithChallenge(one, base, antminerLogPath, creds, nil, "text/plain, */*;q=0.1")
	if trace != nil {
		// Do not pass raw log lines even to the diagnostic redactor.
		trace(antminerLogPath, "GET Digest", httpStatus(err), nil, err, time.Since(started))
	}
	if err == nil {
		if clue := lastAntminerLogClue(data); clue.token != "" {
			addHealthFault(&r, clue.category, clue.description, "Antminer "+antminerLogPath, "code="+clue.token, "log")
		}
	}
	return r
}

type antminerLogClue struct{ token, category, description string }

var (
	antLogDatePrefix = regexp.MustCompile(`^\[?\d{4}-\d{2}-\d{2}[t ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:z|[+-]\d{2}:?\d{2})?\]?\s+`)
	antLogLevel      = regexp.MustCompile(`^\s*(?:\[\s*\d+(?:\.\d+)?\s*\]|\[(?:info|notice|warning|warn|error|debug)\]|<[0-7]>)\s*`)
	antLogBoundary   = regexp.MustCompile(`^(?:linux version\s|(?:start mining(?: mode)?|start cgminer|restart cgminer|miner started|hashrate recovered)[.!]?\s*$)`)
	antSweepError    = regexp.MustCompile(`\bsw\s*eep error string\s*=\s*([a-z]:[0-9]+)\b`)
	antMissingASIC   = regexp.MustCompile(`\bchain\s+[0-9]{1,2}\s+only find\s+[0-9]{1,3}\s+asics?\s*,\s*will power off hash\s*board\b`)
	// Sources: S19-family Kernel Log troubleshooting and S19 XP sweep codes.
	// https://support.bitmain.com/hc/en-us/articles/4406645619097
	// https://support.bitmain.com/hc/en-us/articles/10845468564633
	antSweepClues = map[string]antminerLogClue{
		"v:1": {"SWEEP_V:1", "power", "电源或供电异常"},
		"f:1": {"SWEEP_F:1", "fan", "风扇异常"},
		"p:1": {"SWEEP_P:1", "overheat", "触发高温保护"},
		"j:6": {"SWEEP_J:6", "temperature", "温度传感异常"},
	}
)

func lastAntminerLogClue(data []byte) antminerLogClue {
	data = bytes.TrimSpace(data)
	if json.Valid(data) {
		// A few HTTP front-ends quote plain log text. Ignore arbitrary JSON
		// objects, whose fields may just be errors or configuration.
		var quoted string
		if json.Unmarshal(data, &quoted) != nil {
			return antminerLogClue{}
		}
		data = []byte(strings.TrimSpace(quoted))
	}
	// Ignore HTML login/error pages. A '<4>' kernel priority prefix is valid.
	if len(data) > 1 && data[0] == '<' && !(data[1] >= '0' && data[1] <= '7') {
		return antminerLogClue{}
	}
	const tailBytes = 64 << 10
	if len(data) > tailBytes {
		data = data[len(data)-tailBytes:]
		if nl := bytes.IndexByte(data, '\n'); nl >= 0 {
			data = data[nl+1:]
		} else {
			return antminerLogClue{}
		}
	}
	var last antminerLogClue
	for raw := range strings.SplitSeq(string(data), "\n") {
		if len(raw) > 4096 {
			continue
		}
		line := strings.ToLower(strings.TrimSpace(raw))
		line = antLogDatePrefix.ReplaceAllString(line, "")
		for {
			clean := antLogLevel.ReplaceAllString(line, "")
			if clean == line {
				break
			}
			line = clean
		}
		if antLogBoundary.MatchString(line) {
			last = antminerLogClue{}
			continue
		}
		if match := antSweepError.FindStringSubmatch(line); len(match) > 1 {
			if clue, known := antSweepClues[match[1]]; known {
				last = clue
			}
			continue
		}
		switch {
		case strings.Contains(line, "error_fan_lost"):
			last = antminerLogClue{"ERROR_FAN_LOST", "fan", "风扇掉速或未检测到风扇"}
		case strings.Contains(line, "fail to read pic temp"):
			last = antminerLogClue{"PIC_TEMP_READ_FAILED", "temperature", "算力板 PIC 温度读取失败"}
		case antMissingASIC.MatchString(line):
			last = antminerLogClue{"ASIC_NOT_FOUND", "board", "ASIC 芯片未完整识别，算力板已停止"}
		case strings.Contains(line, "over max temp"):
			last = antminerLogClue{"OVER_MAX_TEMP", "overheat", "触发高温保护"}
		}
	}
	return last
}
