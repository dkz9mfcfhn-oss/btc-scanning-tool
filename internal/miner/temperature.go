package miner

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"minerwatch/internal/telemetry"
)

func tempValue(v any) *float64 { return telemetry.Valid(numeric(v)) }
func tempSamples(v any) []*float64 {
	var items []any
	switch a := v.(type) {
	case []any:
		items = a
	case string:
		if strings.Contains(a, ",") {
			for _, part := range strings.Split(a, ",") {
				part = strings.TrimSpace(part)
				if strings.Contains(part, " ") {
					for _, field := range strings.Fields(part) {
						items = append(items, field)
					}
				} else {
					items = append(items, part)
				}
			}
		} else {
			for _, field := range strings.Fields(a) {
				items = append(items, field)
			}
		}
	case nil:
		return nil
	default:
		items = []any{v}
	}
	out := make([]*float64, 0, len(items))
	for _, item := range items {
		out = append(out, tempValue(item))
	}
	return out
}
func samplesSeries(v any, source string) telemetry.TemperatureSeries {
	return telemetry.Complete(telemetry.TemperatureSeries{SensorsC: tempSamples(v), Source: source})
}
func rangeSeries(v any, source string) telemetry.TemperatureSeries {
	if m, ok := v.(map[string]any); ok {
		return telemetry.Complete(telemetry.TemperatureSeries{AverageC: tempValue(m["avg"]), MinC: tempValue(m["min"]), MaxC: tempValue(m["max"]), Source: source})
	}
	return samplesSeries(v, source)
}
func seriesPresent(s telemetry.TemperatureSeries) bool {
	if s.AverageC != nil || s.MinC != nil || s.MaxC != nil {
		return true
	}
	for _, v := range s.SensorsC {
		if v != nil {
			return true
		}
	}
	return false
}
func boardPresent(b telemetry.BoardTemperature) bool {
	if seriesPresent(b.Chip) || seriesPresent(b.PCB) {
		return true
	}
	for _, v := range b.Other {
		if v.ValueC != nil {
			return true
		}
	}
	return false
}
func hasBoardTemperatures(r Reading) bool {
	for _, b := range r.Boards {
		if boardPresent(b) {
			return true
		}
	}
	return false
}
func addSensor(out *[]telemetry.Sensor, name, source string, v any) {
	if n := tempValue(v); n != nil {
		*out = telemetry.MergeSensors(*out, []telemetry.Sensor{{Name: name, Source: source, ValueC: n}})
	}
}
func addSampleSensors(out *[]telemetry.Sensor, name, source string, v any) {
	for i, n := range tempSamples(v) {
		if n != nil {
			*out = telemetry.MergeSensors(*out, []telemetry.Sensor{{Name: fmt.Sprintf("%s · 传感器%d", name, i), Source: fmt.Sprintf("%s[%d]", source, i), ValueC: n}})
		}
	}
}

var integerID = regexp.MustCompile(`^[0-9]{1,5}$`)

func rawID(v any) (string, bool) {
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		if integerID.MatchString(s) {
			n, _ := strconv.Atoi(s)
			if n <= 65535 {
				return s, true
			}
		}
		return "", false
	}
	if n := numeric(v); n != nil && *n >= 0 && *n <= 65535 && float64(int(*n)) == *n {
		return strconv.Itoa(int(*n)), true
	}
	return "", false
}
func boardIdentity(m map[string]any, index int, keys ...string) (id, name string) {
	for _, k := range keys {
		if n, ok := rawID(m[k]); ok {
			return k + ":" + n, k + "=" + n
		}
	}
	return "position:" + strconv.Itoa(index), fmt.Sprintf("序列位置%d（板号未上报）", index)
}

func mergeSeries(a, b telemetry.TemperatureSeries) telemetry.TemperatureSeries {
	if a.Source == "" {
		return telemetry.CloneSeries(b)
	}
	if b.Source != "" && a.Source != b.Source {
		return telemetry.CloneSeries(a)
	}
	if a.AverageC == nil {
		a.AverageC = b.AverageC
		a.AverageKind = b.AverageKind
	}
	if a.MinC == nil {
		a.MinC = b.MinC
	}
	if a.MaxC == nil {
		a.MaxC = b.MaxC
	}
	if len(a.SensorsC) == 0 {
		a.SensorsC = b.SensorsC
	} else if len(a.SensorsC) == len(b.SensorsC) {
		for i := range a.SensorsC {
			if a.SensorsC[i] == nil {
				a.SensorsC[i] = b.SensorsC[i]
			}
		}
	}
	return telemetry.Complete(telemetry.CloneSeries(a))
}
func mergeBoards(a, b []telemetry.BoardTemperature) []telemetry.BoardTemperature {
	out := telemetry.CloneBoards(a)
	for _, candidate := range b {
		found := false
		for i := range out {
			if out[i].ID != candidate.ID {
				continue
			}
			out[i].Chip = mergeSeries(out[i].Chip, candidate.Chip)
			out[i].PCB = mergeSeries(out[i].PCB, candidate.PCB)
			out[i].Other = telemetry.MergeSensors(out[i].Other, candidate.Other)
			found = true
			break
		}
		if !found {
			out = append(out, telemetry.CloneBoards([]telemetry.BoardTemperature{candidate})[0])
		}
	}
	return out
}

// collectTemperatures computes the legacy headline only from accepted chip/PCB
// data. Ambient/PSU/water readings remain separately named sensors.
func collectTemperatures(r Reading) Reading {
	r.TempC = telemetry.Valid(r.TempC)
	r.InletC = telemetry.Valid(r.InletC)
	r.OutletC = telemetry.Valid(r.OutletC)
	for _, b := range r.Boards {
		r.TempC = maximum(r.TempC, b.Chip.MaxC)
		if !seriesPresent(b.Chip) {
			r.TempC = maximum(r.TempC, b.PCB.MaxC)
		}
	}
	return r
}

var legacyTempField = regexp.MustCompile(`^(temp2_|temp3_|temperature|temp_pcb_?|temp_chip_?|temp)([0-9]+)$`)

// Some Antminer-compatible status APIs encode positive sensor readings with
// hyphens. Restrict this format to a whole nonnegative numeric list so a signed
// scalar is never split and malformed or missing slots never shift board data.
var antminerHyphenSamples = regexp.MustCompile(`^\s*[0-9]+(?:\.[0-9]+)?(?:\s*-\s*[0-9]+(?:\.[0-9]+)?)+\s*$`)

func antminerSamplesSeries(v any, source string) telemetry.TemperatureSeries {
	if raw, ok := v.(string); ok && antminerHyphenSamples.MatchString(raw) {
		v = strings.ReplaceAll(raw, "-", ",")
	}
	return samplesSeries(v, source)
}

func antminerTemperatures(s map[string]any) Reading {
	r := Reading{}
	// Legacy CGI embeds board fields inside the first devs record's freq string.
	for _, dev := range objects(s["devs"]) {
		fields := map[string]any{}
		for k, v := range dev {
			if legacyTempField.MatchString(k) {
				fields[k] = v
			}
		}
		for _, part := range strings.Split(text(dev, "freq"), ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 && legacyTempField.MatchString(strings.TrimSpace(kv[0])) {
				fields[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			}
		}
		r = merge(r, antminerTemperatures(fields))
	}
	for i, c := range objects(s["chain"]) {
		id, label := boardIdentity(c, i, "index", "id")
		source := "Antminer STATS.chain[" + label + "]"
		b := telemetry.BoardTemperature{ID: "antminer:chain:" + id, Name: "链 " + label + "（CGI/CGMiner）", Chip: antminerSamplesSeries(c["temp_chip"], source+".temp_chip"), PCB: antminerSamplesSeries(c["temp_pcb"], source+".temp_pcb")}
		addSampleSensors(&b.Other, "PIC温度", source+".temp_pic", c["temp_pic"])
		// Preserve every reported chain, even when all its temperature slots are null.
		r.Boards = mergeBoards(r.Boards, []telemetry.BoardTemperature{b})
	}
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		match := legacyTempField.FindStringSubmatch(key)
		if len(match) == 0 {
			continue
		}
		if _, ok := rawID(match[2]); !ok {
			continue
		}
		b := telemetry.BoardTemperature{ID: "antminer:legacy:" + match[2], Name: "板号 " + match[2] + "（旧API）"}
		source := "Antminer STATS." + key
		switch match[1] {
		case "temp2_", "temp_chip", "temp_chip_":
			b.Chip = antminerSamplesSeries(s[key], source)
		case "temp_pcb", "temp_pcb_":
			b.PCB = antminerSamplesSeries(s[key], source)
		default:
			addSampleSensors(&b.Other, key+"（固件温度字段）", source, s[key])
		}
		r.Boards = mergeBoards(r.Boards, []telemetry.BoardTemperature{b})
	}
	addSensor(&r.Temperatures, "环境温度", "Antminer STATS.ambient_temp", s["ambient_temp"])
	addSampleSensors(&r.Temperatures, "未分配板号的芯片温度", "Antminer STATS.temp_chip", s["temp_chip"])
	addSampleSensors(&r.Temperatures, "未分配板号的PCB温度", "Antminer STATS.temp_pcb", s["temp_pcb"])
	return collectTemperatures(r)
}

func whatsminerLegacyTemperatures(devs []map[string]any) Reading {
	r := Reading{}
	for i, d := range devs {
		id, label := boardIdentity(d, i, "Slot", "ASC")
		if asc, ok := rawID(d["ASC"]); ok && strings.HasPrefix(id, "Slot:") {
			id += ":ASC:" + asc
			label += " / ASC=" + asc
		}
		source := "Whatsminer DEVS[" + label + "]"
		b := telemetry.BoardTemperature{ID: "whatsminer:legacy:" + id, Name: "板 " + label + "（旧API）", Chip: telemetry.Complete(telemetry.TemperatureSeries{AverageC: tempValue(d["Chip Temp Avg"]), MinC: tempValue(d["Chip Temp Min"]), MaxC: tempValue(d["Chip Temp Max"]), Source: source + ".Chip Temp"})}
		addSensor(&b.Other, "板出风口温度（非芯片均温）", source+".Temperature", d["Temperature"])
		r.Boards = mergeBoards(r.Boards, []telemetry.BoardTemperature{b})
	}
	return collectTemperatures(r)
}
func whatsminerV3Temperatures(msg map[string]any) Reading {
	r := Reading{}
	for _, s := range objects(msg["summary"]) {
		for _, f := range []struct{ key, name string }{{"chip-temp-min", "整机芯片最低温"}, {"chip-temp-avg", "整机芯片均温"}, {"chip-temp-max", "整机芯片最高温"}, {"environment-temperature", "环境温度"}, {"water-temp-in", "进水温度"}, {"water-temperature-in", "进水温度"}, {"water-temp-out", "出水温度"}, {"water-temperature-out", "出水温度"}} {
			addSensor(&r.Temperatures, f.name, "Whatsminer v3 summary."+f.key, s[f.key])
		}
		addSampleSensors(&r.Temperatures, "板温序列（未映射板号）", "Whatsminer v3 summary.board-temperature", s["board-temperature"])
	}
	for i, d := range objects(msg["edevs"]) {
		id, label := boardIdentity(d, i, "slot", "id")
		if n, ok := rawID(d["id"]); ok && strings.HasPrefix(id, "slot:") {
			id += ":id:" + n
			label += " / id=" + n
		}
		source := "Whatsminer v3 edevs[" + label + "]"
		b := telemetry.BoardTemperature{ID: "whatsminer:v3:" + id, Name: "板 " + label + "（API v3）", Chip: telemetry.Complete(telemetry.TemperatureSeries{AverageC: tempValue(d["chip-temp-avg"]), MinC: tempValue(d["chip-temp-min"]), MaxC: tempValue(d["chip-temp-max"]), Source: source + ".chip-temp"})}
		addSampleSensors(&b.Other, "板温传感器", source+".board-temperature", d["board-temperature"])
		r.Boards = mergeBoards(r.Boards, []telemetry.BoardTemperature{b})
	}
	return collectTemperatures(r)
}

var avalonModuleID = regexp.MustCompile(`^MM ID([0-9]{1,5})$`)
var avalonPVTT = regexp.MustCompile(`^PVT_T([0-9]{1,3})$`)

func avalonTemperatures(stats map[string]any) Reading {
	r := Reading{}
	keys := make([]string, 0, len(stats))
	for k := range stats {
		if avalonModuleID.MatchString(k) || k == avalonFlatKey {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, moduleKey := range keys {
		raw, ok := stats[moduleKey].(string)
		if !ok {
			continue
		}
		module := ""
		if match := avalonModuleID.FindStringSubmatch(moduleKey); len(match) > 1 {
			module = match[1]
		}
		sourceKey := moduleKey
		if moduleKey == avalonFlatKey {
			sourceKey = "STATS"
		}
		fields := map[string]string{}
		for _, m := range bracket.FindAllStringSubmatch(raw, -1) {
			fields[m[1]] = m[2]
		}
		av, max := tempSamples(fields["MTavg"]), tempSamples(fields["MTmax"])
		indices := map[int]bool{}
		for i := range av {
			indices[i] = true
		}
		for i := range max {
			indices[i] = true
		}
		for key := range fields {
			if m := avalonPVTT.FindStringSubmatch(key); len(m) > 0 {
				n, _ := strconv.Atoi(m[1])
				indices[n] = true
			}
		}
		sorted := make([]int, 0, len(indices))
		for i := range indices {
			sorted = append(sorted, i)
		}
		sort.Ints(sorted)
		for _, i := range sorted {
			source := fmt.Sprintf("Avalon %s.MTavg[%d]/MTmax[%d]/PVT_T%d", sourceKey, i, i, i)
			series := telemetry.TemperatureSeries{Source: source, SensorsC: tempSamples(fields["PVT_T"+strconv.Itoa(i)])}
			if i < len(av) {
				series.AverageC = av[i]
			}
			if i < len(max) {
				series.MaxC = max[i]
			}
			series = telemetry.Complete(series)
			b := telemetry.BoardTemperature{ID: fmt.Sprintf("avalon:mm:%s:board:%d", module, i), Name: fmt.Sprintf("模块%s · 板%d（MM日志）", module, i), Chip: series}
			if moduleKey == avalonFlatKey {
				b.ID = fmt.Sprintf("avalon:stats:board:%d", i)
				b.Name = fmt.Sprintf("板%d（STATS日志）", i)
			}
			r.Boards = mergeBoards(r.Boards, []telemetry.BoardTemperature{b})
		}
		for _, f := range []struct{ key, name string }{{"Temp", "环境温度"}, {"TAvg", "模块芯片均温"}, {"TMax", "模块芯片最高温"}, {"ITemp", "入口温度（固件ITemp，非默认水温）"}, {"OTemp", "出口温度（固件OTemp，非默认水温）"}} {
			name := "模块" + module + " · " + f.name
			if moduleKey == avalonFlatKey {
				name = strings.ReplaceAll(f.name, "模块", "整机")
			}
			addSensor(&r.Temperatures, name, "Avalon "+sourceKey+"."+f.key, fields[f.key])
		}
	}
	return collectTemperatures(r)
}

func byteonTemperatures(m map[string]any) Reading {
	r := Reading{}
	for _, f := range []struct{ key, name string }{{"chip_temp", "整机芯片"}, {"pcb_temp", "整机PCB"}} {
		if v, ok := m[f.key].(map[string]any); ok {
			for _, stat := range []struct{ key, name string }{{"min", "最低温"}, {"max", "最高温"}, {"avg", "均温"}} {
				addSensor(&r.Temperatures, f.name+stat.name, "REST miner."+f.key+"."+stat.key, v[stat.key])
			}
		}
	}
	for i, c := range objects(m["chains"]) {
		id, label := boardIdentity(c, i, "id", "index", "chain_id")
		source := "REST miner.chains[" + label + "]"
		b := telemetry.BoardTemperature{ID: "byteon:chain:" + id, Name: "链 " + label + "（兼容REST）", Chip: rangeSeries(c["chip_temp"], source+".chip_temp"), PCB: rangeSeries(c["pcb_temp"], source+".pcb_temp")}
		if _, ok := c["chip_temp_sens"]; ok {
			b.Chip = restSensorSeries(c["chip_temp_sens"], source+".chip_temp_sens")
		}
		if _, ok := c["pcb_temp_sens"]; ok {
			b.PCB = restSensorSeries(c["pcb_temp_sens"], source+".pcb_temp_sens")
		}
		addSampleSensors(&b.Other, "PIC温度", source+".temp_pic", c["temp_pic"])
		r.Boards = mergeBoards(r.Boards, []telemetry.BoardTemperature{b})
	}
	return collectTemperatures(r)
}

// Only readings with status=measure are current in the published REST schema.
func restSensorSeries(v any, source string) telemetry.TemperatureSeries {
	list, ok := v.([]any)
	if !ok {
		return samplesSeries(nil, source)
	}
	values := make([]*float64, len(list))
	for i, item := range list {
		sensor, ok := item.(map[string]any)
		if ok && sensor["status"] == "measure" {
			values[i] = tempValue(sensor["temp"])
		}
	}
	return telemetry.Complete(telemetry.TemperatureSeries{SensorsC: values, Source: source})
}
func hasBoardMeans(r Reading) bool {
	if len(r.Boards) == 0 {
		return false
	}
	for _, b := range r.Boards {
		if b.Chip.AverageC == nil {
			return false
		}
	}
	return true
}
