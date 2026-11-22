package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"minerwatch/internal/miner"
	"minerwatch/internal/network"
	"minerwatch/internal/store"
	"minerwatch/internal/telemetry"
)

const interval = 5 * time.Minute

type Config struct {
	ElectricityCNY     *float64        `json:"electricityCnyPerKwh,omitempty"`
	PoolFeePercent     float64         `json:"poolFeePercent,omitempty"`
	ExceptionFirst     bool            `json:"exceptionFirst"`
	CloudWatchDisabled bool            `json:"cloudWatchDisabled,omitempty"`
	ListSort           string          `json:"listSort,omitempty"`
	ManualOrder        []string        `json:"manualOrder,omitempty"`
	FullColumns        bool            `json:"fullColumns,omitempty"`
	ColumnVisibility   map[string]bool `json:"columnVisibility,omitempty"`
	Theme              string          `json:"theme,omitempty"`
	Language           string          `json:"language,omitempty"`
	UIScalePercent     int             `json:"uiScalePercent,omitempty"`
	DashboardWidths    *cardWidths     `json:"dashboardCardWidths,omitempty"`
	IntervalSeconds    int             `json:"intervalSeconds"`
	Ranges             string          `json:"ranges"`
	Username           string          `json:"username"`
	Concurrency        int             `json:"concurrency"`
	TimeoutSeconds     int             `json:"timeoutSeconds"`
}
type Device struct {
	ManualPowerW *float64       `json:"manualPowerW,omitempty"`
	Note         string         `json:"note,omitempty"`
	Hidden       bool           `json:"hidden,omitempty"`
	WorkerNames  []string       `json:"workerNames,omitempty"`
	WorkerReadAt time.Time      `json:"workerReadAt,omitempty"`
	IP           string         `json:"ip"`
	Brand        string         `json:"brand"`
	Model        string         `json:"model"`
	Protocol     string         `json:"protocol"`
	Label        string         `json:"label"`
	LastSeen     time.Time      `json:"lastSeen"`
	Latest       *store.Sample  `json:"latest"`
	Sparkline    []store.Sample `json:"sparkline,omitempty"`
}
type metadata struct {
	Config  Config    `json:"config"`
	Devices []*Device `json:"devices"`
}
type Job struct {
	Running bool   `json:"running"`
	Kind    string `json:"kind"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Found   int    `json:"found"`
	Error   string `json:"error"`
}
type App struct {
	market            marketQuote
	marketIO          sync.Mutex
	marketNext        time.Time
	marketRunning     bool
	fleetCache        fleetHistoryCache
	cloudIO           sync.Mutex
	cloudRunning      bool
	cloudNext         time.Time
	cloudStatus       string
	cloudID           string
	dailyState        dailyReportState
	dailyRunning      bool
	dailyNextAttempt  time.Time
	dailyError        string
	dailySender       func(context.Context, string, string, string) error
	pushURL           string
	pushError         string
	pushStates        map[string]pushState
	mu                sync.Mutex
	jobs              sync.WaitGroup
	ctx               context.Context
	cancel            context.CancelFunc
	dir               string
	demo              bool
	db                *store.Store
	config            Config
	password          string
	devices           map[string]*Device
	job               Job
	paused            bool
	diagnosticRunning bool
	nextPoll          time.Time
	storageError      string
	discover          func(context.Context, string) bool
	probe             func(context.Context, string, miner.Credentials) (miner.Reading, error)
}

func newApp(dir string, demo bool) (*App, error) {
	return newAppContext(context.Background(), dir, demo)
}

func newAppContext(ctx context.Context, dir string, demo bool) (*App, error) {
	db, err := store.OpenContext(ctx, filepath.Join(dir, "history"))
	if err != nil {
		return nil, err
	}
	ready := false
	defer func() {
		if !ready {
			db.Close()
		}
	}()
	a := &App{password: "admin", dir: dir, demo: demo, db: db, devices: map[string]*Device{}, config: Config{ExceptionFirst: true, Username: "admin", Concurrency: 24, TimeoutSeconds: 4}, nextPoll: time.Now().Add(interval), probe: miner.Probe, discover: minerPortReachable}
	a.storageError = db.Warning()
	a.loadPush()
	a.loadDailyReport()
	a.loadCloudIdentity()
	p, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err == nil {
		var m metadata
		if err = json.Unmarshal(p, &m); err != nil {
			return nil, fmt.Errorf("settings.json invalid: %w", err)
		}
		if err = validateConfig(m.Config); err != nil {
			return nil, err
		}
		a.config = m.Config
		// Existing installs gain exception priority once; explicit off persists.
		var saved struct {
			Config map[string]json.RawMessage `json:"config"`
		}
		if json.Unmarshal(p, &saved) == nil {
			if _, ok := saved.Config["exceptionFirst"]; !ok {
				a.config.ExceptionFirst = true
			}
		}
		for _, d := range m.Devices {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if ips, e := network.ExpandRanges(d.IP); e == nil && len(ips) == 1 && ips[0] == d.IP {
				d.Sparkline = nil
				a.devices[d.IP] = d
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	for _, d := range a.devices {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if s, ok := db.Latest(d.IP); ok {
			d.Latest = &s
		}
	}
	if demo {
		if err := a.makeDemo(ctx); err != nil {
			return nil, err
		}
	} else if len(a.devices) > 0 {
		a.nextPoll = time.Now().Add(time.Second)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ready = true
	return a, nil
}
func validateConfig(c Config) error {
	if !validPoolFee(c.PoolFeePercent) {
		return errors.New("矿池手续费应为 0–100%")
	}
	if c.ElectricityCNY != nil && !validTariff(*c.ElectricityCNY) {
		return errors.New("电价应为非负数字（元/度）")
	}
	if c.IntervalSeconds != 0 && (c.IntervalSeconds < 30 || c.IntervalSeconds > 1800) {
		return errors.New("采集间隔应为30–1800秒")
	}
	if c.Concurrency < 1 || c.Concurrency > 128 {
		return errors.New("并发数量应为 1–128")
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 10 {
		return errors.New("连接超时应为 1–10 秒")
	}
	if len(c.Ranges) > 32768 || len(c.Username) > 128 {
		return errors.New("设置内容过长")
	}
	if c.Ranges != "" {
		_, err := network.ExpandRanges(c.Ranges)
		return err
	}
	return nil
}
func (a *App) saveLocked() error {
	m := metadata{Config: a.config, Devices: []*Device{}}
	for _, d := range a.devices {
		v := *d
		v.Sparkline = nil
		m.Devices = append(m.Devices, &v)
	}
	sort.Slice(m.Devices, func(i, j int) bool { return m.Devices[i].IP < m.Devices[j].IP })
	p, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(a.dir, "settings.tmp")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(p)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(a.dir, "settings.json"))
	}
	return err
}
func (a *App) schedule(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	maintenance := time.NewTicker(time.Minute)
	defer maintenance.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-maintenance.C:
			if err := a.db.Maintain(now); err != nil {
				a.mu.Lock()
				a.storageError = "历史清理失败：" + err.Error()
				a.mu.Unlock()
			}
		case now := <-t.C:
			a.maybeRefreshMarket(ctx, now)
			a.maybeDailyReport(ctx, now)
			a.maybeCloudHeartbeat(ctx, now)
			a.mu.Lock()
			due := !a.paused && !a.diagnosticRunning && !a.job.Running && len(a.devices) > 0 && !time.Now().Before(a.nextPoll)
			a.mu.Unlock()
			if due {
				_ = a.startJob("poll")
			}
		}
	}
}
func (a *App) startJob(kind string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx != nil && a.ctx.Err() != nil {
		return errors.New("程序正在退出")
	}
	if a.diagnosticRunning {
		return errors.New("正在进行采集诊断，请等待完成")
	}
	if a.job.Running {
		return errors.New("正在采集中，请等待当前任务完成")
	}
	if a.demo {
		a.nextPoll = time.Now().Add(a.config.pollInterval())
		return nil
	}
	var ips []string
	if kind == "scan" {
		var err error
		ips, err = network.ExpandRanges(a.config.Ranges)
		if err != nil {
			return err
		}
	} else {
		for ip := range a.devices {
			ips = append(ips, ip)
		}
	}
	if len(ips) == 0 {
		return errors.New("请先设置 IP 网段并扫描矿机")
	}
	a.job = Job{Running: true, Kind: kind, Total: len(ips)}
	a.nextPoll = time.Now().Add(a.config.pollInterval())
	conf := a.config
	creds := miner.Credentials{Username: conf.Username, Password: a.password}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	a.jobs.Add(1)
	go func() { defer a.jobs.Done(); a.runJob(ctx, ips, conf, creds, kind) }()
	return nil
}
func (a *App) runJob(ctx context.Context, ips []string, c Config, creds miner.Credentials, kind string) {
	queue := make(chan scanTarget, len(ips))
	if kind == "scan" {
		go a.enqueueScanTargets(ctx, ips, c.Concurrency, queue)
	} else {
		for _, ip := range ips {
			queue <- scanTarget{ip: ip, known: true}
		}
		close(queue)
	}
	var workers sync.WaitGroup
	count := min(c.Concurrency, 32)
	retrySlots := make(chan struct{}, max(4, count/2))
	const historyBatchSize = 16
	records := make([]store.Record, 0, historyBatchSize)
	flushHistory := func() {
		if len(records) == 0 {
			return
		}
		if err := a.db.Append(records); err != nil {
			a.storageError = "历史数据保存失败：" + err.Error()
		}
		clear(records)
		records = records[:0]
	}
	if count > len(ips) {
		count = len(ips)
	}
	for i := 0; i < count; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for target := range queue {
				ip := target.ip
				r, err := readReliably(ctx, ip, creds, c.TimeoutSeconds, a.probe, retrySlots, 700*time.Millisecond, target.known)
				// Do not turn application shutdown into false offline history.
				if ctx.Err() != nil {
					return
				}
				s := store.Sample{IntervalSeconds: int(c.pollInterval() / time.Second), At: time.Now().UTC(), Online: err == nil, ElapsedSeconds: r.ElapsedSeconds, RealtimeTH: r.RealtimeTH, AverageTH: r.AverageTH, TempC: r.TempC, InletC: r.InletC, OutletC: r.OutletC, Accepted: r.Accepted, Rejected: r.Rejected, RejectPercent: r.RejectPercent, HashrateSource: r.HashrateSource, Warnings: r.Warnings, Boards: telemetry.CloneBoards(r.Boards), Temperatures: telemetry.CloneSensors(r.Temperatures)}
				if err != nil {
					s.Error = err.Error()
				} else if validMeasurement(r.PowerW) {
					s.PowerW, s.PowerSource = r.PowerW, r.PowerSource
				}
				s.Health = telemetry.AssessHashrate(r.RealtimeTH, err == nil, r.Faults, s.Error)
				a.mu.Lock()
				a.job.Done++
				d, known := a.devices[ip]
				if !known && err == nil {
					d = &Device{IP: ip}
					a.devices[ip] = d
					known = true
				}
				if known {
					if err == nil {
						if _, manual := store.ResolvePower(s.PowerW, s.PowerSource, d.ManualPowerW); manual {
							s.ManualPowerW = d.ManualPowerW
						}
						d.Brand = r.Brand
						d.Model = r.Model
						d.Protocol = r.Protocol
						d.LastSeen = s.At
						if r.WorkersKnown {
							d.WorkerNames = append([]string(nil), r.WorkerNames...)
							d.WorkerReadAt = s.At
						}
						a.job.Found++
					}
					d.Latest = &s
					records = append(records, store.Record{IP: ip, Sample: s})
					if len(records) == historyBatchSize {
						flushHistory()
					}
				}
				a.mu.Unlock()
			}
		}()
	}
	workers.Wait()
	a.mu.Lock()
	flushHistory()
	a.mu.Unlock()
	a.notifyScan(ctx, ips)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.job.Running = false
	if ctx.Err() != nil {
		a.job.Error = "任务已停止"
	}
	if err := a.saveLocked(); err != nil {
		a.storageError = "设置保存失败：" + err.Error()
	}
	if !a.nextPoll.After(time.Now()) {
		a.job.Error = "本次任务超过所选采集间隔；请缩小网段或提高并发，空档不补数据。"
		a.nextPoll = time.Now().Add(time.Second)
	}
}
func fresh(s *store.Sample, now time.Time) bool {
	return s != nil && s.Online && now.Sub(s.At) <= 2*sampleInterval(s) && now.Sub(s.At) >= -time.Minute
}
func num(v float64) *float64 { return &v }
func (a *App) makeDemo(ctx context.Context) error {
	now := time.Now().UTC().Truncate(interval)
	names := []string{"Antminer", "Whatsminer", "Avalon"}
	models := []string{"S19", "M63S", "A1566HA"}
	rates := []float64{95, 390, 460}
	records := []store.Record{}
	for i := 0; i < 12; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		b := i % 3
		ip := fmt.Sprintf("192.168.10.%d", 101+i)
		d := &Device{IP: ip, Brand: names[b], Model: models[b], Protocol: "演示", Label: fmt.Sprintf("A区 · %02d号", i+1)}
		for j := 0; j < 288; j++ {
			at := now.Add(time.Duration(j-287) * interval)
			online := !(i == 4 && j >= 276) && !(i == 2 && j > 130 && j < 138)
			s := store.Sample{IntervalSeconds: 300, At: at, Online: online, Warnings: []string{}, HashrateSource: "演示采样"}
			if online {
				s.PowerW, s.PowerSource = num([]float64{3200, 7200, 9900}[b]), "演示采样"
				s.RealtimeTH = num(rates[b] * (1 + 0.025*math.Sin(float64(j+i*13)/9)))
				s.AverageTH = num(rates[b] * (1 + 0.007*math.Sin(float64(j)/25)))
				s.TempC = num(64 + float64(i) + 3*math.Sin(float64(j)/40))
				s.Boards, s.Temperatures = demoTemperatures(*s.TempC)
				s.Accepted = num(float64(j*100 + i*7 + 1))
				s.Rejected = num(float64(j/30 + i%3))
				if b == 1 {
					s.InletC = num(31)
					s.OutletC = num(42)
				}
				d.LastSeen = at
			} else {
				s.Error = "演示：连接超时"
			}
			var faults []telemetry.Fault
			if i == 11 && j >= 286 && online {
				s.RealtimeTH = num(0)
				faults = []telemetry.Fault{{Code: "starting", Message: "演示：矿机正在启动", Source: "演示数据/运行状态", Evidence: "演示状态=starting", Confidence: "reported"}}
			}
			s.Health = telemetry.AssessHashrate(s.RealtimeTH, s.Online, faults, s.Error)
			records = append(records, store.Record{IP: ip, Sample: s})
			if j == 287 {
				d.Latest = &s
			}
		}
		a.devices[ip] = d
	}
	// Demo uses its own directory and records; existing matching points are avoided.
	if err := ctx.Err(); err != nil {
		return err
	}
	if !a.db.HasSamplesSince(now.Add(-24 * time.Hour)) {
		if err := a.db.Append(records); err != nil {
			a.storageError = err.Error()
		}
	}
	return nil
}

// These values belong exclusively to the isolated demonstration fixture. Real
// samples only copy temperatures supplied by the protocol collectors above.
func demoTemperatures(highest float64) ([]telemetry.BoardTemperature, []telemetry.Sensor) {
	boards := make([]telemetry.BoardTemperature, 0, 3)
	for board := 0; board < 3; board++ {
		chipMean := highest - 6 + float64(board*2)
		pcbMean := chipMean - 14
		boards = append(boards, telemetry.BoardTemperature{
			ID: fmt.Sprintf("demo-board-%d", board+1), Name: fmt.Sprintf("演示算力板 %d", board+1),
			Chip: telemetry.TemperatureSeries{AverageC: num(chipMean), MinC: num(chipMean - 2), MaxC: num(chipMean + 2), SensorsC: []*float64{num(chipMean - 2), num(chipMean), num(chipMean + 2)}, AverageKind: "reported", Source: fmt.Sprintf("演示数据/board%d/chip", board+1)},
			PCB:  telemetry.TemperatureSeries{AverageC: num(pcbMean), MinC: num(pcbMean - 1), MaxC: num(pcbMean + 1), SensorsC: []*float64{num(pcbMean - 1), num(pcbMean), num(pcbMean + 1)}, AverageKind: "reported", Source: fmt.Sprintf("演示数据/board%d/pcb", board+1)},
		})
	}
	return boards, []telemetry.Sensor{{Name: "演示环境温度", ValueC: num(26), Source: "演示数据/ambient"}}
}

func (c Config) pollInterval() time.Duration {
	if c.IntervalSeconds == 0 {
		return interval
	}
	return time.Duration(c.IntervalSeconds) * time.Second
}
func sampleInterval(s *store.Sample) time.Duration {
	if s == nil || s.IntervalSeconds < 30 {
		return interval
	}
	return time.Duration(s.IntervalSeconds) * time.Second
}
func (a *App) setInterval(seconds int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.config
	c := old
	c.IntervalSeconds = seconds
	if err := validateConfig(c); err != nil {
		return err
	}
	a.config = c
	if err := a.saveLocked(); err != nil {
		a.config = old
		return err
	}
	a.nextPoll = time.Now().Add(c.pollInterval())
	return nil
}
