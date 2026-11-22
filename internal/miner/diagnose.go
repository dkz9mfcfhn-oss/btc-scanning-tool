package miner

import (
	"context"
	"encoding/json"
	"errors"
	"minerwatch/internal/telemetry"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type diagnosticStep struct {
	ParserInputBytes int            `json:"parserInputBytes,omitempty"`
	HasMTavg         bool           `json:"hasMTavg"`
	ParsedTempC      *float64       `json:"parsedTempC,omitempty"`
	Endpoint         string         `json:"endpoint"`
	Method           string         `json:"method"`
	StatusCode       int            `json:"statusCode,omitempty"`
	ElapsedMS        int64          `json:"elapsedMs"`
	Error            string         `json:"error,omitempty"`
	Telemetry        map[string]any `json:"telemetry,omitempty"`
	ParseError       string         `json:"parseError,omitempty"`
	ParsedBoards     int            `json:"parsedBoards"`
	ParsedBoardMeans int            `json:"parsedBoardMeans"`
}

type diagnosticMeasurements struct {
	Health        telemetry.HashrateHealth     `json:"health"`
	Boards        []telemetry.BoardTemperature `json:"boards,omitempty"`
	Temperatures  []telemetry.Sensor           `json:"temperatures,omitempty"`
	Brand         string                       `json:"brand,omitempty"`
	Model         string                       `json:"model,omitempty"`
	RealtimeTH    *float64                     `json:"realtimeTH"`
	AverageTH     *float64                     `json:"averageTH"`
	TempC         *float64                     `json:"tempC"`
	Accepted      *float64                     `json:"accepted"`
	Rejected      *float64                     `json:"rejected"`
	RejectPercent *float64                     `json:"rejectPercent"`
}

type diagnosticReport struct {
	CollectorVersion      string                 `json:"collectorVersion"`
	SchemaVersion         int                    `json:"schemaVersion"`
	GeneratedAt           time.Time              `json:"generatedAt"`
	TargetIP              string                 `json:"targetIP"`
	ReadOnly              bool                   `json:"readOnly"`
	Redaction             string                 `json:"redaction"`
	Steps                 []diagnosticStep       `json:"steps"`
	Measurements          diagnosticMeasurements `json:"measurements"`
	TemperatureConclusion string                 `json:"temperatureConclusion"`
	Incomplete            bool                   `json:"incomplete"`
}

// Diagnose reads a fixed list of status endpoints and returns a redacted report.
// It does not save files. The only POST permitted is the documented REST unlock
// authentication after known firmware identity has been established. It never
// exports passwords, bearer tokens, pool/user/wallet records, raw HTML or logs.
// The caller must serialize this with regular polling for the same device.
func Diagnose(ctx context.Context, ip string, creds Credentials) ([]byte, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() == nil {
		return nil, errors.New("请输入 IPv4 地址")
	}
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	report := diagnosticReport{CollectorVersion: "MineWatch 2.31", SchemaVersion: 4, GeneratedAt: time.Now().UTC(), TargetIP: ip, ReadOnly: true, Redaction: "仅保留设备型号、固件家族、数值遥测及安全分类的状态依据；密码、令牌、矿池地址、钱包、用户、网络配置和原始日志均不导出", Steps: []diagnosticStep{}}
	aggregate := Reading{}
	record := func(endpoint, method string, status int, data []byte, err error, elapsed time.Duration) {
		step := diagnosticStep{ParserInputBytes: len(data), HasMTavg: strings.Contains(string(data), "MTavg"), Endpoint: endpoint, Method: method, StatusCode: status, ElapsedMS: elapsed.Milliseconds(), Error: diagnosticError(err)}
		if endpoint != "/api/v1/unlock" {
			step.Telemetry = withoutPower(redactTelemetry(data))
		}

		if err == nil && endpoint != antminerLogPath && (strings.HasPrefix(endpoint, "tcp:4028/") || strings.HasPrefix(endpoint, "/cgi-bin/") || endpoint == "/updatecglog.cgi") {
			parsed, parseErr := ParseCGMiner(data)
			if endpoint == "tcp:4028/stats?new_api=true" {
				parsed, parseErr = parseAvalonFMS(data)
				step.Telemetry = nil
				if parseErr == nil {
					step.Telemetry = map[string]any{"boards": parsed.Boards, "temperatures": parsed.Temperatures}
				}
			}
			if strings.HasPrefix(endpoint, "/cgi-bin/") {
				parsed, parseErr = ParseAntminer(data)
			}
			if parseErr != nil {
				step.ParseError = "unrecognized_status_response"
			} else {
				step.ParsedTempC = parsed.TempC
				step.ParsedBoards = len(parsed.Boards)
				for _, b := range parsed.Boards {
					if b.Chip.AverageC != nil {
						step.ParsedBoardMeans++
					}
				}
			}
		}
		report.Steps = append(report.Steps, step)
		if err != nil && (status == 0 || status == 401 || status == 403 || status >= 500) {
			report.Incomplete = true
		}
	}
	// Identify 1566 before legacy HTTP probes can consume the diagnostic budget.
	identityCtx, identityCancel := context.WithTimeout(ctx, 3*time.Second)
	identityData, identityErr := tcpJSON(identityCtx, net.JoinHostPort(ip, "4028"), map[string]string{"command": "version"}, false)
	identityCancel()
	if identityErr == nil {
		if identity, e := ParseCGMiner(identityData); e == nil {
			aggregate = merge(aggregate, identity)
		}
	}
	var authenticatedLog Reading
	httpFamily := detectBTCHTTP(ctx, "http://"+ip)
	avalonRead := strings.HasPrefix(httpFamily, "Avalon")
	if avalonRead && !is1566(aggregate) {
		extra, e := readAvalonHTTPFamily(ctx, "http://"+ip, httpFamily, true, record)
		if e != nil {
			report.Steps = append(report.Steps, diagnosticStep{Endpoint: "Avalon HTTP log parser", Method: "PARSE", Error: diagnosticError(e)})
			report.Incomplete = true
		}
		if e == nil {
			aggregate = merge(aggregate, extra)
			if btcTemperatureRank(extra.Temperatures) == 2 {
				authenticatedLog = extra
			}
		}
	}
	if !avalonRead && !is1566(aggregate) {
		restCtx, restCancel := context.WithTimeout(ctx, 10*time.Second)
		client := restClient()
		restReading, restErr := readByteonTraced(restCtx, client, "http://"+ip, creds, record)
		client.CloseIdleConnections()
		restCancel()
		if restErr == nil {
			aggregate = merge(aggregate, restReading)
		} else if !strings.Contains(restErr.Error(), "endpoint unavailable") {
			report.Steps = append(report.Steps, diagnosticStep{Endpoint: "REST compatibility parser", Method: "PARSE", Error: diagnosticError(restErr)})
			report.Incomplete = true
		}
		for _, path := range []string{"/cgi-bin/stats.cgi", "/cgi-bin/summary.cgi", "/cgi-bin/get_miner_status.cgi", "/cgi-bin/miner_type.cgi", "/cgi-bin/pools.cgi"} {
			if ctx.Err() != nil {
				report.Incomplete = true
				break
			}
			started := time.Now()
			one, cancel := context.WithTimeout(ctx, 4*time.Second)
			data, err := httpJSON(one, "http://"+ip, path, creds)
			cancel()
			code := httpStatus(err)
			record(path, "GET Digest", code, data, err, time.Since(started))
			if err == nil {
				if r, e := ParseAntminer(data); e == nil {
					aggregate = merge(aggregate, r)
				}
			}
		}

	}
	aggregate = merge(aggregate, diagnoseCGMiner(ctx, net.JoinHostPort(ip, "4028"), record))
	if !is1566(aggregate) && !avalonRead && (aggregate.Brand == "Avalon" || aggregate.Brand == "") {
		if r, e := readAvalonHTTP(ctx, "http://"+ip, aggregate.Brand == "Avalon", record); e == nil {
			aggregate = merge(aggregate, r)
			if btcTemperatureRank(r.Temperatures) == 2 {
				authenticatedLog = r
			}
		}
	}
	if ctx.Err() == nil && (aggregate.Brand == "Whatsminer" || !hasData(aggregate)) {
		v3Ctx, v3Cancel := context.WithTimeout(ctx, 4*time.Second)
		var status, info []byte
		for _, command := range []string{"get.miner.status", "get.device.info"} {
			if v3Ctx.Err() != nil {
				report.Incomplete = true
				break
			}
			req := map[string]string{"cmd": command}
			if command == "get.miner.status" {
				req["param"] = "summary+pools+edevs"
			}
			started := time.Now()
			data, err := tcpJSON(v3Ctx, net.JoinHostPort(ip, "4433"), req, true)
			record("tcp:4433/"+command, "READ", 0, data, err, time.Since(started))
			if command == "get.miner.status" {
				status = data
			} else {
				info = data
			}
			if err != nil {
				break
			}
		}
		if r, e := ParseWhatsminerV3(status, info); e == nil {
			aggregate = merge(aggregate, r)
		}
		v3Cancel()
	}
	aggregate = addAntminerLogEvidence(ctx, "http://"+ip, creds, aggregate, record)
	if ctx.Err() != nil {
		report.Incomplete = true
	}
	if btcTemperatureRank(authenticatedLog.Temperatures) == 2 {
		aggregate = mergeFreshAvalonLog(aggregate, authenticatedLog)
	}
	if is1566(aggregate) {
		one, stop := context.WithTimeout(ctx, 6*time.Second)
		started := time.Now()
		data, e := tcpJSON(one, net.JoinHostPort(ip, "4028"), fmsRequest(), false)
		stop()
		record("tcp:4028/stats?new_api=true", "READ", 0, data, e, time.Since(started))
		fresh := Reading{}
		if e == nil {
			fresh, e = parseAvalonFMS(data)
		}
		aggregate = applyAvalonFMS(aggregate, fresh, e)
		if e != nil {
			report.Incomplete = true
		}
	} else {
		correctAvalonOverview(&aggregate)
	}
	if is1566(aggregate) {
		aggregate = supplementAvalonHealth(ctx, "http://"+ip, httpFamily, aggregate, record)
	}
	report.Measurements = diagnosticMeasurements{Boards: telemetry.CloneBoards(aggregate.Boards), Temperatures: telemetry.CloneSensors(aggregate.Temperatures), Brand: safeBrand(aggregate.Brand), Model: safeModel(aggregate.Model), RealtimeTH: aggregate.RealtimeTH, AverageTH: aggregate.AverageTH, TempC: aggregate.TempC, Accepted: aggregate.Accepted, Rejected: aggregate.Rejected, RejectPercent: aggregate.RejectPercent}
	report.Measurements.Health = telemetry.AssessHashrate(aggregate.RealtimeTH, hasObservation(aggregate), aggregate.Faults, "")
	means := 0
	for _, b := range aggregate.Boards {
		if b.Chip.AverageC != nil {
			means++
		}
	}
	if means == 0 {
		report.TemperatureConclusion = "未取得每板芯片均温；保留已返回的单点、极值和原始温度字段，不从最高最低温推算均温"
	} else {
		report.TemperatureConclusion = "已读取每板温度；固件均温与采样点均值分别标注，同一物理板不同接口记录未强行合并"
	}
	return json.MarshalIndent(report, "", "  ")
}

func safeBrand(s string) string {
	switch s {
	case "Antminer", "Avalon", "Whatsminer", "CGMiner":
		return s
	}
	return ""
}

var modelPattern = regexp.MustCompile(`(?i)\b(?:Antminer\s+|Avalon(?:Miner)?\s+|Whatsminer\s+)?(?:[STM][0-9]{1,4}[a-z0-9+_-]*(?:\s+(?:pro|xp|hydro|ha|plus))?|[0-9]{4}[a-z]{0,3})\b`)

func safeModel(s string) string {
	if len(s) > 256 {
		return ""
	}
	return modelPattern.FindString(s)
}

var statusPattern = regexp.MustCompile(`HTTP ([1-5][0-9]{2})`)

func httpStatus(err error) int {
	if err == nil {
		return 200
	}
	if match := statusPattern.FindStringSubmatch(err.Error()); len(match) > 1 {
		n, _ := strconv.Atoi(match[1])
		return n
	}
	if strings.Contains(err.Error(), "用户名") || strings.Contains(err.Error(), "认证") {
		return 401
	}
	return 0
}
func diagnosticError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return "timeout"
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "标识") || strings.Contains(s, "identity"):
		return "firmware_identity_unrecognized"
	case strings.Contains(s, "MTavg"):
		return "avalon_mtavg_not_found"
	case strings.Contains(s, "measurements") || strings.Contains(s, "miner object"):
		return "telemetry_schema_unrecognized"
	case strings.Contains(s, "2 MiB"):
		return "response_too_large"
	case strings.Contains(s, "JSON") || strings.Contains(s, "json"):
		return "invalid_json_response"
	case strings.Contains(s, "redirect"):
		return "redirect_refused"
	case strings.Contains(s, "解锁") || strings.Contains(s, "用户名") || strings.Contains(s, "认证") || strings.Contains(s, "Digest"):
		return "authentication_failed_or_unsupported"
	}
	if match := statusPattern.FindStringSubmatch(s); len(match) > 1 {
		return "http_" + match[1]
	}
	return "connection_or_protocol_error"
}

var sensitiveField = regexp.MustCompile(`(?i)pass|token|pool|user|wallet|account|secret|auth|cookie|session|salt|nonce|mac|serial|(^|[_ -])sn($|[_ -])|url|hostname|address|network|credential|key`)
var telemetryField = regexp.MustCompile(`(?i)power|watt|volt|current|temp|hashrate|^hr_|ghs|mhs|ths|^hs rt$|^rate_|frequency|^freq|^elapsed$|^fan|^chip|^psu$|^ps$|^mtavg$|^mtmax$|^pvt_t[0-9]+$`)
var safeFieldName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 _()/.-]{0,63}$`)
var versionPattern = regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}(?:\.[0-9]{1,3})?`)

func redactTelemetry(data []byte) map[string]any {
	if len(data) == 0 {
		return nil
	}
	m, e := decode(data)
	if e != nil {
		m = parseLegacy(string(data))
	}
	if len(m) == 0 {
		return nil
	}
	normalizeAvalonStats(m)
	value := redactNode(m, "", false, 0)
	out, _ := value.(map[string]any)
	return out
}
func redactNode(v any, key string, inMetric bool, depth int) any {
	if depth > 8 {
		return nil
	}
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, item := range x {
			if !safeFieldName.MatchString(k) || sensitiveField.MatchString(k) {
				continue
			}
			if strings.HasPrefix(k, "MM ID") || k == avalonFlatKey {
				if s, ok := item.(string); ok {
					if mm := redactAvalonModule(s); len(mm) > 0 {
						out[k] = mm
					}
				}
				continue
			}
			childMetric := inMetric || telemetryField.MatchString(k)
			switch strings.ToLower(k) {
			case "id", "index", "slot", "asc":
				if id, ok := rawID(item); ok {
					out[k] = id
				}
				continue
			}
			knownContainer := false
			switch strings.ToLower(k) {
			case "stats", "summary", "version", "devdetails", "devs", "miner", "info", "data", "result", "msg", "chain", "chains", "edevs", "firmware", "fw":
				knownContainer = true
			}
			if !childMetric && !knownContainer && !isIdentityField(k) {
				continue
			}
			if value := redactNode(item, k, childMetric, depth+1); value != nil {
				out[k] = value
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := []any{}
		for i, item := range x {
			if i >= 64 {
				break
			}
			value := redactNode(item, key, inMetric, depth+1)
			if value != nil || inMetric {
				out = append(out, value)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case string:
		switch strings.ToLower(key) {
		case "status":
			if inMetric {
				switch x {
				case "measure", "init", "error", "unknown":
					return x
				}
			}
			return nil
		case "fw_name", "firmware_name", "firmware", "fw", "name":
			if f := familyName(x); f != "" {
				return f
			}
			return nil
		case "miner", "model", "miner_type", "minertype", "type", "prod":
			if m := safeModel(x); m != "" {
				return m
			}
			return nil
		case "hr_measure", "rate_unit":
			switch x {
			case "TH/s", "GH/s", "MH/s", "KH/s", "PH/s", "H/s":
				return x
			}
			return nil
		case "fw_version":
			if v := versionPattern.FindString(x); v != "" {
				return v
			}
			return nil
		}
		if inMetric {
			if strings.Contains(strings.ToLower(key), "temp") && antminerHyphenSamples.MatchString(x) {
				return antminerSamplesSeries(x, "").SensorsC
			}
			if n := numeric(x); n != nil {
				return *n
			}
			if strings.Contains(strings.ToLower(key), "temp") {
				values := tempSamples(x)
				for _, n := range values {
					if n != nil {
						return values
					}
				}
			}
		}
	case float64:
		if inMetric {
			return x
		}
	case bool:
		// Numeric telemetry is enough for this report; state and configuration flags
		// are omitted to avoid leaking authorization/configuration details.
	}
	return nil
}
func isIdentityField(key string) bool {
	switch strings.ToLower(key) {
	case "fw_name", "firmware_name", "fw_version", "hr_measure", "rate_unit", "miner", "model", "miner_type", "minertype", "type", "prod", "name":
		return true
	}
	return false
}
func redactAvalonModule(s string) map[string]any {
	out := map[string]any{}
	for _, match := range bracket.FindAllStringSubmatch(s, -1) {
		if avalonPVTT.MatchString(match[1]) {
			out[match[1]] = tempSamples(match[2])
			continue
		}
		switch match[1] {
		case "GHSmm", "GHSavg", "TMax", "TAvg", "Temp", "PS", "MTmax", "MTavg", "ITemp", "OTemp":
			values := []any{}
			for _, part := range strings.Fields(match[2]) {
				if n := numeric(part); n != nil {
					values = append(values, *n)
				}
			}
			if len(values) > 0 {
				out[match[1]] = values
			}
		}
	}
	return out
}

func withoutPower(m map[string]any) map[string]any {
	for k, v := range m {
		lower := strings.ToLower(k)
		if strings.Contains(lower, "power") || strings.Contains(lower, "watt") || lower == "ps" || lower == "pin" {
			delete(m, k)
			continue
		}
		switch child := v.(type) {
		case map[string]any:
			m[k] = withoutPower(child)
		case []any:
			for i, x := range child {
				if obj, ok := x.(map[string]any); ok {
					child[i] = withoutPower(obj)
				}
			}
		}
	}
	return m
}

// Each read has its own deadline. A refused or unsupported command must not
// suppress later temperature endpoints on a device with a working connection.
func diagnoseCGMiner(ctx context.Context, address string, record restTrace) Reading {
	aggregate := Reading{}
	for _, command := range []string{"stats", "estats", "summary", "edevs", "version", "devdetails", "pools"} {
		if ctx.Err() != nil {
			break
		}
		started := time.Now()
		one, cancel := context.WithTimeout(ctx, 3*time.Second)
		data, err := tcpJSON(one, address, map[string]string{"command": command, "cmd": command}, false)
		cancel()
		record("tcp:4028/"+command, "READ", 0, data, err, time.Since(started))
		if err == nil {
			if r, e := ParseCGMiner(data); e == nil {
				aggregate = merge(aggregate, r)
			}
		}
	}
	return aggregate
}
