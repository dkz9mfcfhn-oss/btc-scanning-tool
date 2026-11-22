package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"minerwatch/internal/network"
	"minerwatch/internal/store"
	"minerwatch/internal/telemetry"
)

func guard(host string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.Host != host {
			fail(w, 403, "仅允许通过本机地址访问")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+host {
				fail(w, 403, "来源无效")
				return
			}
			if r.Method != "GET" && r.Method != "HEAD" {
				if r.Method != "POST" {
					fail(w, 405, "方法无效")
					return
				}
				if r.Header.Get("X-MinerWatch") != "1" {
					fail(w, 403, "请求标记缺失")
					return
				}
				if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
					fail(w, 415, "需要 JSON 请求")
					return
				}
				r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
			}
		}
		next.ServeHTTP(w, r)
	})
}
func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "请求参数无效："+err.Error())
		return false
	}
	return true
}
func (a *App) routes(m *http.ServeMux) {
	m.HandleFunc("GET /api/state", a.state)
	m.HandleFunc("GET /api/history", a.history)
	m.HandleFunc("GET /api/export", a.export)
	m.HandleFunc("POST /api/config", a.configure)
	m.HandleFunc("POST /api/scan", func(w http.ResponseWriter, r *http.Request) { a.begin(w, "scan") })
	m.HandleFunc("POST /api/poll", func(w http.ResponseWriter, r *http.Request) { a.begin(w, "poll") })
	m.HandleFunc("POST /api/monitor", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Paused bool `json:"paused"`
		}
		if !decode(w, r, &v) {
			return
		}
		a.mu.Lock()
		a.paused = v.Paused
		if !v.Paused {
			a.nextPoll = time.Now()
		}
		a.mu.Unlock()
		respond(w, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /api/miner", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			IP    string `json:"ip"`
			Label string `json:"label"`
		}
		if !decode(w, r, &v) {
			return
		}
		if len([]rune(v.Label)) > 80 {
			fail(w, 400, "备注最多 80 字")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		d, ok := a.devices[v.IP]
		if !ok {
			fail(w, 404, "矿机不存在")
			return
		}
		old := d.Label
		d.Label = strings.TrimSpace(v.Label)
		if err := a.saveLocked(); err != nil {
			d.Label = old
			fail(w, 500, err.Error())
			return
		}
		respond(w, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /api/remove", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			IP string `json:"ip"`
		}
		if !decode(w, r, &v) {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.job.Running {
			fail(w, 409, "采集期间无法移除矿机")
			return
		}
		d, ok := a.devices[v.IP]
		if !ok {
			fail(w, 404, "矿机不存在")
			return
		}
		delete(a.devices, v.IP)
		if err := a.saveLocked(); err != nil {
			a.devices[v.IP] = d
			fail(w, 500, err.Error())
			return
		}
		respond(w, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /api/shutdown", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]bool{"ok": true})
		go func() { time.Sleep(300 * time.Millisecond); a.cancel() }()
	})
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "接口不存在或方法无效") })
}

type Totals struct {
	PowerW           float64  `json:"powerW"`
	PowerReported    int      `json:"powerReported"`
	PowerManual      int      `json:"powerManual"`
	Count            int      `json:"count"`
	Online           int      `json:"online"`
	Offline          int      `json:"offline"`
	Stale            int      `json:"stale"`
	RealtimeTH       float64  `json:"realtimeTH"`
	AverageTH        float64  `json:"averageTH"`
	Accepted         float64  `json:"accepted"`
	Rejected         float64  `json:"rejected"`
	RejectPercent    *float64 `json:"rejectPercent"`
	RejectReported   int      `json:"rejectReported"`
	RealtimeReported int      `json:"realtimeReported"`
	AverageReported  int      `json:"averageReported"`
	MaxTempC         *float64 `json:"maxTempC"`
}
type FleetPoint struct {
	At              time.Time `json:"at"`
	RealtimeTH      *float64  `json:"realtimeTH"`
	Online          int       `json:"online"`
	Count           int       `json:"count"`
	Reported        int       `json:"reported"`
	RejectPercent   *float64  `json:"rejectPercent"`
	RejectReported  int       `json:"rejectReported"`
	Observed        int       `json:"observed"`
	IntervalSeconds int       `json:"intervalSeconds"`
}

func totals(devices []*Device, now time.Time) Totals {
	var t Totals
	for _, d := range devices {
		if d == nil || d.Hidden {
			continue
		}
		t.Count++
		s := d.Latest
		if s == nil || now.Sub(s.At) > 2*sampleInterval(s) {
			t.Stale++
			continue
		}
		if !s.Online {
			t.Offline++
			continue
		}
		if !fresh(s, now) {
			t.Stale++
			continue
		}
		t.Online++
		if watts, manual := store.ResolvePower(s.PowerW, s.PowerSource, d.ManualPowerW); watts != nil {
			t.PowerW += *watts
			if manual {
				t.PowerManual++
			} else {
				t.PowerReported++
			}
		}
		if s.RealtimeTH != nil {
			t.RealtimeTH += *s.RealtimeTH
			t.RealtimeReported++
		}
		if s.AverageTH != nil {
			t.AverageTH += *s.AverageTH
			t.AverageReported++
		}
		if validShares(s) {
			t.Accepted += *s.Accepted
			t.Rejected += *s.Rejected
			t.RejectReported++
		}
		if s.TempC != nil && (t.MaxTempC == nil || *s.TempC > *t.MaxTempC) {
			t.MaxTempC = num(*s.TempC)
		}
	}
	t.RejectPercent = sharePercent(t.Accepted, t.Rejected)
	return t
}
func (a *App) state(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	devices := []*Device{}
	for _, d := range a.devices {
		v := *d
		v.Sparkline = a.db.History(d.IP, now.Add(-24*time.Hour))
		devices = append(devices, &v)
	}
	sort.Slice(devices, func(i, j int) bool { return ipLess(devices[i].IP, devices[j].IP) })
	cfg := map[string]any{"ranges": a.config.Ranges, "username": a.config.Username, "hasPassword": a.password != "", "concurrency": a.config.Concurrency, "timeoutSeconds": a.config.TimeoutSeconds, "intervalSeconds": int(a.config.pollInterval() / time.Second)}
	respond(w, map[string]any{"demo": a.demo, "version": "1.0.0", "intervalSeconds": int(a.config.pollInterval() / time.Second), "now": now, "nextPoll": a.nextPoll, "paused": a.paused, "dataDir": a.dir, "storageError": a.storageError, "config": cfg, "job": a.job, "miners": devices, "totals": totals(devices, now), "fleetHistory": fleetHistory(a.db.All(now.Add(-24*time.Hour)), a.devices), "suggestedRanges": network.LocalRanges()})
}
func ipLess(a, b string) bool {
	aa := strings.Split(a, ".")
	bb := strings.Split(b, ".")
	for i := 0; i < 4; i++ {
		x, _ := strconv.Atoi(aa[i])
		y, _ := strconv.Atoi(bb[i])
		if x != y {
			return x < y
		}
	}
	return false
}
func (a *App) history(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	a.mu.Lock()
	_, ok := a.devices[ip]
	a.mu.Unlock()
	if !ok {
		fail(w, 404, "矿机不存在")
		return
	}
	respond(w, map[string]any{"ip": ip, "samples": a.db.History(ip, time.Now().Add(-24*time.Hour))})
}
func csvNumber(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', 3, 64)
}
func csvSafe(s string) string {
	if strings.ContainsAny(s[:min(1, len(s))], "=+-@\t\r") {
		return "'" + s
	}
	return s
}
func csvJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return csvSafe("JSON 序列化失败：" + err.Error())
	}
	return csvSafe(string(data))
}
func (a *App) export(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	a.mu.Lock()
	known := map[string]string{}
	for k, d := range a.devices {
		known[k] = d.Model
	}
	a.mu.Unlock()
	if ip != "" {
		if _, ok := known[ip]; !ok {
			fail(w, 404, "矿机不存在")
			return
		}
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="minewatch-24h.csv"`)
	fmt.Fprint(w, "\xEF\xBB\xBF")
	c := csv.NewWriter(w)
	defer c.Flush()
	c.Write([]string{"采样时间(UTC)", "IP", "型号", "在线", "实时算力(TH/s)", "固件平均算力(TH/s)", "温度概览(℃)", "进水温度(℃)", "出水温度(℃)", "拒绝率(%)", "接受份额", "说明", "每板温度(JSON)", "其他温度(JSON)", "算力状态", "无算力原因", "依据(JSON)", "拒绝份额"})
	for _, v := range a.db.All(time.Now().Add(-24 * time.Hour)) {
		model, ok := known[v.IP]
		if !ok || (ip != "" && v.IP != ip) {
			continue
		}
		s := v.Sample
		health := s.Health
		if health.State == "" {
			// Old samples have no recorded fault evidence. Derive only what the
			// saved rate/connectivity permits; do not invent a past cause.
			health = telemetry.AssessHashrate(s.RealtimeTH, s.Online, nil, s.Error)
		}
		c.Write([]string{s.At.Format(time.RFC3339), v.IP, csvSafe(model), strconv.FormatBool(s.Online), csvNumber(s.RealtimeTH), csvNumber(s.AverageTH), csvNumber(s.TempC), csvNumber(s.InletC), csvNumber(s.OutletC), csvNumber(rejection(&s)), csvNumber(s.Accepted), csvSafe(s.Error), csvJSON(s.Boards), csvJSON(s.Temperatures), csvSafe(health.State), csvSafe(health.Reason), csvJSON(health.Evidence), csvNumber(s.Rejected)})
	}
}
func (a *App) begin(w http.ResponseWriter, kind string) {
	if a.demo {
		fail(w, 400, "演示模式不扫描网络，请启动正式程序")
		return
	}
	if err := a.startJob(kind); err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, map[string]bool{"ok": true})
}
func (a *App) configure(w http.ResponseWriter, r *http.Request) {
	var v struct {
		IntervalSeconds int     `json:"intervalSeconds"`
		Ranges          string  `json:"ranges"`
		Username        string  `json:"username"`
		Password        *string `json:"password"`
		ClearPassword   bool    `json:"clearPassword"`
		Concurrency     int     `json:"concurrency"`
		TimeoutSeconds  int     `json:"timeoutSeconds"`
	}
	if !decode(w, r, &v) {
		return
	}
	if a.demo {
		fail(w, 400, "演示模式不能修改连接设置")
		return
	}
	c := Config{IntervalSeconds: v.IntervalSeconds, Ranges: strings.TrimSpace(v.Ranges), Username: v.Username, Concurrency: v.Concurrency, TimeoutSeconds: v.TimeoutSeconds}
	if err := validateConfig(c); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if v.Password != nil && len(*v.Password) > 1024 {
		fail(w, 400, "密码过长")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.job.Running {
		fail(w, 409, "请等待当前采集结束再修改设置")
		return
	}
	old := a.config
	c.ElectricityCNY, c.PoolFeePercent = old.ElectricityCNY, old.PoolFeePercent
	c.Theme, c.UIScalePercent, c.DashboardWidths = old.Theme, old.UIScalePercent, old.DashboardWidths
	c.Language = old.Language
	c.ExceptionFirst, c.CloudWatchDisabled = old.ExceptionFirst, old.CloudWatchDisabled
	c.FullColumns, c.ColumnVisibility = old.FullColumns, old.ColumnVisibility
	c.ListSort, c.ManualOrder = old.ListSort, old.ManualOrder
	if c.IntervalSeconds == 0 {
		c.IntervalSeconds = old.IntervalSeconds
	}
	a.config = c
	if err := a.saveLocked(); err != nil {
		a.config = old
		fail(w, 500, err.Error())
		return
	}
	if v.ClearPassword {
		a.password = ""
	} else if v.Password != nil {
		a.password = *v.Password
	}
	respond(w, map[string]bool{"ok": true})
}
