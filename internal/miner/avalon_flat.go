package miner

import (
	"regexp"
	"sort"
	"strings"
)

// Internal container for firmware logs that have no MM module identifier.
const avalonFlatKey = "Avalon STATS log"

func isAvalonFlat(raw string) bool {
	fields := map[string]string{}
	for _, m := range bracket.FindAllStringSubmatch(raw, -1) {
		fields[m[1]] = m[2]
	}
	return fields["Ver"] != "" &&
		(fields["MTavg"] != "" || fields["SYSTEMSTATU"] != "" || fields["BOOTBY"] != "") &&
		(fields["GHSavg"] != "" || fields["GHSmm"] != "" || fields["GHSspd"] != "")
}

var avalonModuleLabel = regexp.MustCompile(`^MM ID\s*([0-9]{1,5})(?:\s|:|\(|$)`)

func normalizeAvalonStats(m map[string]any) {
	for _, key := range []string{"STATS", "stats"} {
		if raw, ok := m[key].(string); ok && isAvalonFlat(raw) {
			m[key] = []any{map[string]any{avalonFlatKey: raw}}
		}
		for _, stats := range objects(m[key]) {
			keys := make([]string, 0, len(stats))
			for k := range stats {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				raw, ok := stats[k].(string)
				if !ok {
					continue
				}
				if match := avalonModuleLabel.FindStringSubmatch(strings.TrimSpace(k)); len(match) > 1 {
					canonical := "MM ID" + match[1]
					if canonical != k {
						if _, exists := stats[canonical]; !exists {
							stats[canonical] = raw
							delete(stats, k)
						}
					}
					continue
				}
				if k != avalonFlatKey && isAvalonFlat(raw) {
					if _, exists := stats[avalonFlatKey]; !exists {
						stats[avalonFlatKey] = raw
					}
				}
			}
		}
	}
}

func flatAvalonLegacy(raw string) map[string]any {
	s := strings.TrimSpace(raw)
	// Accept the supplied log notation, including an omitted closing brace.
	for _, prefix := range []string{"'STATS':", "\"STATS\":", "STATS:"} {
		if strings.HasPrefix(s, prefix) {
			body := strings.TrimSpace(strings.TrimPrefix(s, prefix))
			if strings.HasPrefix(body, "{") && isAvalonFlat(body) {
				return map[string]any{"STATS": []any{map[string]any{avalonFlatKey: body}}}
			}
		}
	}
	return nil
}
