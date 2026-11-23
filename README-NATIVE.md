# BTC scanning tool V1.3 Windows native desktop

Windows 10/11 x64 standalone GUI. Native Win32 controls and GDI; no browser or external runtime is required by the Windows executable. `main.go` is the separate non-Windows legacy test/demo entry point, excluded by build constraints on Windows.

V1.3 adds hosted ntfy.sh for Android alerts and the morning report. Configure and test it in Phone alerts. Cloud watchdog alerts still use the supported Bark service.

V1.2 accumulates matched net income from the most recent 08:00 Beijing time until the next 08:00. The earnings details show the period and the time through which it has been calculated. The morning report covers the completed period.

V1.1 shows the pool fee for the intervals used in net income and adjusts the estimate for share rejection. The dashboard and Bark morning report use the same calculation.

V1.0 is the first GitHub release. The Windows x64 executable is attached to the [release page](https://github.com/dkz9mfcfhn-oss/btc-scanning-tool/releases/tag/V1.0).

2.56 adds the **软件来源** / **GitHub source** entry next to the dashboard title. It opens the project's [GitHub repository](https://github.com/dkz9mfcfhn-oss/btc-scanning-tool) in the default browser.

2.55 adds flat White, Graphite, and Sand themes and a language menu next to the zoom options. Chinese, English, and Russian can be switched while monitoring, and the app saves both preferences. Device data and report files keep their existing format.

2.54 converts the BTC/USDT spot quote through the exchange's EUR/USDT market and a daily EUR/CNY reference rate, with a second reference-rate feed as backup. It can use the direct BTC/EUR spot quote when the cross is unavailable, and retains the final direct BTC/CNY quote fallback.

2.53 prefers the public BTC/USDT spot ticker and converts it using a USDT/CNY quote. If either part is unavailable, it falls back to a direct BTC/CNY quote.

2.52 matches the net income label and amount to the typography and text colors of the other four dashboard cards.

2.51 makes the 24-hour net income and its yuan amount the main figures on the fifth card and uses smaller text for power, fees, and settings.

2.50 moves power and estimated income into the fifth card in the top row. Drag the divider between two cards to resize them and save your layout. Double-click any divider to restore the default widths. Open 收益设置 in the fifth card to view the full 24-hour breakdown.

2.49 puts the electricity tariff and farm-wide pool fee under 收益设置. Net income deducts both charges. Right-click a miner to set a manual power fallback in watts. The current list uses it for fresh online devices without usable input power, and subsequent samples preserve it separately from reported watts. The 08:00 Bark report includes the fee and identifies any kWh estimated from manual power.

2.48 collects reported power and shows 24-hour estimated BTC revenue in CNY, electricity cost, and net income. Set the electricity price in yuan per kWh under 电价设置. The dashboard values measured history at the latest public BTC/CNY spot price and network difficulty using the block subsidy. The 08:00 Beijing Bark report uses the fixed preceding 24 hours. Net income uses intervals with both power and hashrate, with coverage shown. Avalon uses WALLPOWER first and labels PS fallback readings as reference power. Missing samples remain gaps.

2.47 centers the 24-hour reject-rate chart vertically using the observed range. Its tick labels stay within 0–100%.

2.46 places the three 24-hour fleet charts on one shared glass surface. The individual chart frames and shadows are removed.

2.45 extends the miner list to the scroll area across window sizes and zoom levels. The dashboard uses a consistent set of line icons for its controls and metric cards. The Windows release uses the same Authenticode publisher as 2.44.

2.44 strengthens the in-app glass with clearer faces, directional highlights, beveled refraction and pill-shaped controls. It takes visual cues from [Apple’s Liquid Glass design guidance](https://developer.apple.com/videos/play/wwdc2025/219/). Hovering updates the active control material. The main windows and child controls draw the same backdrop in physical pixels so fractional scaling and scrolling cover the right and bottom edges. Narrow layouts use smaller, symmetric margins.

2.43 paints a lightweight startup window before reading settings and history. A cancellable background loader prepares saved readings and the initial 24-hour statistics. Startup history reads reuse input buffers and copy only the newest reading per miner into the live list. Native controls are positioned before their first full repaint. Glass rasterization uses reusable lighting tables and writes surface pixels directly into the Windows bitmap.

2.42 supplements fault evidence only after an explicit zero realtime reading. Stock Antminer uses the current kernel log. Avalon displays `BOOTBY` as the last restart record, separate from the current reason. Bark zero-hash notifications still require two consecutive measured zeros. See the protocol notes for classification and firmware limitations.

2.41 reserves space for the complete application name and moves exception priority beside zoom on wide layouts. The dashboard uses translucent rounded layers with edge reflections. Rounded controls composite onto their parent backdrop, and the native list uses a rounded viewport. The Windows implementation blends premultiplied BGRA surfaces with [AlphaBlend](https://learn.microsoft.com/en-us/windows/win32/api/wingdi/nf-wingdi-alphablend).

2.40 displays the name BTC Scanning tool in the native app, Windows file properties, and local Bark notifications. The release uses the selected Gold Scope icon. The existing Windows data folder and Bark group preserve continuity with previous MineWatch installations.

2.39 shows rolling 24-hour average hashrate on the dashboard. Each visible miner contributes its time-weighted mean from valid realtime readings in the window. The card shows the contributing device count and coverage. The dashboard recomputes from in-memory history about every 15 seconds and when the visible device set changes. Morning Bark reports use the same calculation for the previous 08:00 to the current 08:00 Beijing time. A late launch can read the retained disk history for that fixed window. A history read error appears in the Bark settings and the scheduler retries the report.

Application zoom now ranges from 50% through 200%. Cards and controls wrap into rows as the logical window width decreases. Native scrollbars keep remaining overflow accessible, and narrow chart panels use short labels. The three Windows icon designs can be generated with `tools/make_icon_options.swift`.

2.38 assigns the former blank search field beside the Miner List heading to actual scan progress. A single rounded progress track displays completed-target percentage and, when space permits, counts; completion remains visible and stopped/error states are distinguished. Search moves to the next row with an explicit Search label; the previous thin lower-right progress bar is removed. The existing 1-second timer invalidates the progress region only when the job changes and the main window is visible. Full miner-table updates and network cadence remain unchanged. Geometry tests ensure no native control covers the progress region and that search stays separated from the table, including scaled/scrollable layouts. A shared-layout design preview was inspected; it is not a Windows runtime screenshot.

2.37 adds an upper-right Interface Zoom menu: 75/90/100/110/125/150/175/200%. Text and content scale together immediately in every open window; the outer window frame continues using Windows monitor DPI. The saved `uiScalePercent` preference multiplies monitor DPI, with 100% retaining normal system scaling. Ctrl+Plus/Minus steps through sizes; Ctrl+0 restores 100%, including when a large zoom puts the menu off screen. Existing settings without the preference default to 100%. Unsupported stored values fall back to 100%; failed saves roll back without changing the displayed zoom. Tests cover persistence/reopening, failed-save rollback, zoom bounds, composition with system DPI and accessibility of controls under zoom. Native Windows interaction remains unverified on this macOS development host.

2.36 adds per-window DPI scaling. DPI awareness is set before creating any windows (Per Monitor V2, with checked API fallbacks). Controls, fonts, list row heights, column widths, GDI text, charts and glass tiles share 96-DPI logical geometry. Each top-level window handles [WM_DPICHANGED](https://learn.microsoft.com/en-us/windows/win32/hidpi/wm-dpichanged), updates its own resources and uses the suggested bounds clamped to the destination monitor work area. Glass raster destinations use [StretchDIBits logical units](https://learn.microsoft.com/en-us/windows/win32/api/wingdi/nf-wingdi-stretchdibits). Dialog fonts remain independent when windows occupy monitors with different scaling.

On a small logical viewport, the UI keeps readable typography and exposes native horizontal/vertical scrollbars instead of clipping controls. The main canvas minimum is 1100×640 logical units; it is not a single-screen compact rearrangement below that size. The detail, Bark, note and diagnostic windows also have scrollable minimum canvases. Keyboard focus reveals off-screen controls, the blank background supports wheel scrolling (Shift for horizontal), and all utility windows support resizing. Scroll offsets reset when enlarging the window makes them unnecessary. Window bounds use the current monitor, including negative desktop coordinates and display/work-area changes. No timer or continuous scaling loop was introduced.

Validation includes a 49-case physical-resolution/scaling matrix (1024×768 through 3840×2160 and ultrawide; 100/125/150/175/200/250/300%), normal/expanded lists, reachable control edges, dialog canvases, signed coordinates and bounds, plus full race regression and Windows cross-build/static checks. These are code/geometry checks, not Windows runtime, visual or mixed-monitor acceptance. A real Windows run remains required to verify those OS-dependent behaviors.

2.35 refines the native dashboard: a quieter glass palette, larger summary figures with explicit TH/s or PH/s units, rounded input frames, and primary telemetry columns before long notes. A shared geometry model keeps the chart above the status bar at short window heights. It retains all columns and existing hide settings. Child-control material sampling now follows the shared window backdrop. The phone and exception-priority controls have distinct command IDs, fixing the 2.34 routing collision. The visible-row trend cache uses compact metric history. No animated repaint loop was added.

`MINEWATCH_DASHBOARD_PREVIEW=/absolute/path.svg go test -run TestExportDashboardDesignPreview .` exports a design preview using the same geometry, palette, columns and glass renderer. It contains fixture data and is not a Windows runtime screenshot.

2.32 adds persistent numeric IP / natural worker-name sorting, both ascending and descending. Click the IP or worker-name column header, or choose an order in List Settings. Worker names fall back to the location label; unnamed devices stay at the end. The row context menu moves selected miners up/down/to the top/bottom, switches to manual order and saves it. Moves operate within the current filtered list, preserving other devices' positions; new discoveries append to a saved manual order. Hidden miners remain excluded from totals and notifications.

The native interface now draws frosted glass-style cards, reflected edges, soft shadows, pill buttons, tinted status badges and a matching sortable table header. The new Glass palette is the default when no explicit theme is saved; existing theme choices remain selectable. Materials are opaque gradients for readability and Windows 10 compatibility, not Apple's compositor or real desktop blur. Main-window painting is buffered. Header custom draw is handled in its parent list control through a subclass, following the [Windows custom-draw notification contract](https://learn.microsoft.com/en-us/windows/win32/controls/nm-customdraw). Windows visual and interaction acceptance still requires a real Windows run.

2.33 strengthens the glass material with a procedural soft-color backdrop, translucent color blending, edge refraction and antialiased highlights. Native BGRA tiles are cached with a 32 MiB / 64-entry bound. This is an in-app glass material, not a live blur of other desktop windows.

2.34 opens a validated miner HTTP URL in the system default browser. MineWatch no longer fills or submits backend credentials, launches isolated browser sessions, or includes chromedp dependencies. The browser's own saved-password behavior remains under the user's control. Collection credentials are still used by the miner protocol adapters.

Bark sends one farm summary at 08:00 Beijing time for the previous 08:00–08:00 window. It sums each visible miner's valid-time-weighted realtime mean, reports collected cumulative work in EH and sample coverage, and does not treat unknown samples as zero. Measured zeros count. Hidden miners are excluded again at send time. Delivery state is persisted; failures retry after five minutes and late startup catches up today's report only. A crash after server acceptance but before persistence can repeat a report. Existing offline/zero-hash/recovery notifications remain independent.

Exception-first sorting is enabled for new and upgraded settings; the top toggle disables it and preserves that choice. Manual movement disables it to keep the chosen order. The row context menu edits a persistent 200-character note (also searchable) or opens the fault timeline. The timeline groups retained observations, labels recovery/state changes, and explicitly marks sampling gaps as unknown recovery. In-memory history remains 25 hours; disk retention remains three UTC calendar dates.

A companion Python service under `cloud/` watches app heartbeats. The desktop sends a heartbeat immediately and then every 10 minutes to the paired HTTPS service derived from the user's self-hosted `/minewatch-bark/<key>` URL. The same existing Bark capability is sent only to that host in an Authorization header. No miner IPs, notes or worker names are sent in heartbeats. The server alerts after more than 25 minutes without a heartbeat and once again on recovery. Pause cloud watch before deliberate shutdown/maintenance; closing to tray keeps monitoring alive. The phone dialog displays connection status. The cloud server itself must stay reachable to deliver notifications.

Performance: compact metric snapshots and cached fleet charts avoid deep board/sensor copies on redraw; row charts use metric-only history; hidden/minimized main windows stop refreshing. The status bar reports the native process CPU and working-set memory, excluding the user's browser. Glass texture cache remains bounded at 32 MiB. Benchmarks are in `internal/store/metrics_test.go`; actual Windows resource use and visual acceptance require a Windows run.

Build on Windows (Go 1.24+):
```powershell
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags="-s -w -H windowsgui" -o BTC-scanning-tool-V1.3.exe .
```

Run `go test -race ./...` on a race-supported host. Cross-platform static check: `GOOS=windows GOARCH=amd64 go vet -unsafeptr=false ./...`. Native Win32 callback LPARAM conversions intentionally use unsafe pointers.

2.6 adds evidence-based zero-hashrate state and reasons, status/error adapters, a compact reason column and filter, detail evidence, history and CSV fields. Missing hashrate and failed reads are distinct from measured zero. Refer to internal/miner/README-PROTOCOLS.md for contracts and limitations.

No physical miner or Windows runtime validation has been completed in this development environment. Parser fixtures and cross-compilation do not prove firmware compatibility or GUI rendering.

2.7: native fleet charts for hashrate, online count, cumulative share-count rejection; retained single-device chart toggle. Three persistent palettes (dark/light/blue). Power statistics/display/export retired; internal legacy parser fields retained for history/firmware compatibility. Shares remain optional; percent-only readings never enter fleet weighted counts. See the supplied Chinese usage notes for metric scope.

2.8 adds an Open Backend action before the IP column in both layouts. Native list-view subitem hit testing resolves the clicked row. ShellExecuteW opens the validated IPv4 HTTP URL in the Windows default browser; labels and credentials never enter the URL. Double-click on that action does not open miner detail or launch a second browser request.

2.9: independent stats after summary failure; fresh per-attempt deadlines of at least 36 seconds; at most three sequential attempts with cancellable backoff; job worker cap 12 and retry cap 4; two valid zeros required. No stale telemetry substituted as live.

2.10 refines the native dashboard: rounded cards/buttons, painted per-cell status/action badges, compact default column widths, 38px rows, taller separated fleet chart panels with latest recorded values/time. No collector changes. Windows runtime visual verification remains outstanding.

2.11: Avalon estats gets a fresh four-second phase under the overall probe deadline, independent of the six-second identification phase. Per-board completeness requires all observed boards to have means. User 1566HA raw temperature logs are still required to verify firmware schema.

2.25: embedded native icon, close-to-tray, explicit tray exit and Explorer restart handling. History disk retention is three UTC calendar dates; memory remains 25 hours. A minute maintenance ticker cleans even while paused. No notification service is enabled.

Run `swift tools/make_icon_options.swift /path/to/options --round2` on macOS to generate the icon candidates. Use `04-goldscope.ico` and the matching PNG as `assets/minewatch.ico` and `assets/minewatch.png`. Windows builds link `icon_windows_amd64.syso`. To rebuild it for V1.3, compile the helper with `go -C tools/windows_resources build -o /absolute/path/windows_resources .` and run `windows_resources -ico assets/minewatch.ico -name "BTC scanning tool" -version 1.3.0.0 -product-version V1.3 -o icon_windows_amd64.syso`. Verify the EXE with `python3 tools/verify_windows_icon.py BTC-scanning-tool-V1.3.exe assets/minewatch.ico "BTC scanning tool" 1.3.0.0 V1.3`.
