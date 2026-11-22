# Frontend contract

All API requests are same-origin and JSON. Mutating requests require header `X-MinerWatch: 1`. Errors: HTTP 4xx/5xx JSON `{error:string}`. GET /api/state response:
```
{
 "demo":false, "version":"1.0.0", "intervalSeconds":300,
 "now":"ISO date", "nextPoll":"ISO date", "paused":false,
 "dataDir":"path", "storageError":"",
 "config":{"ranges":"192.168.1.1-192.168.1.254","username":"root","hasPassword":false,"concurrency":24,"timeoutSeconds":4},
 "job":{"running":false,"kind":"scan","done":0,"total":0,"found":0,"error":""},
 "miners":[{"ip":"192.168.1.5","brand":"Antminer","model":"S19","protocol":"TCP 4028","label":"","lastSeen":"ISO date","latest":SAMPLE,"sparkline":[SAMPLE]}],
 "totals":{"count":0,"online":0,"offline":0,"stale":0,"realtimeTH":0,"averageTH":0,"powerW":0,"powerReported":0,"realtimeReported":0,"averageReported":0,"maxTempC":null},
 "fleetHistory":[{"at":"ISO date","realtimeTH":null,"online":0,"count":0}]
}
```
SAMPLE: `{at:ISO,online:bool,realtimeTH:number|null,averageTH:number|null,tempC:number|null,inletC:number|null,outletC:number|null,powerW:number|null,powerSource:string,hashrateSource:string,warnings:[],error:string}`. latest may be null initially. Offline or stale (>10 min) latest values must not be displayed as live. Missing readings shown as —, never as zero. sparkline contains last 24h actual samples, may have fewer than 288. Draw line gaps for offline/null and >7.5 min intervals; never invent history. Actual point at zero must be shown as zero.

GET /api/history?ip=IP => `{ip, samples:[SAMPLE]}` last 24h. GET /api/export?ip=IP => CSV last 24h, omit ip for all. IP URL-encode.

POST /api/config with `{ranges,username,password?,clearPassword?:bool,concurrency,timeoutSeconds}`; omitted password preserves session credential; credentials are in memory only and require re-entry after restart. ranges newline/comma/semicolon separated IPv4, IPv4 start-end (or short last octet), CIDR max total 4096 LAN/private IPv4. Settings saved, password never returned.

POST /api/scan body `{}` begins scan configured ranges and adds discovered miners. POST /api/poll body `{}` polls known miners immediately. Busy jobs return 409. POST /api/monitor body `{paused:bool}` sets pause. POST /api/miner body `{ip,label}` renames device. POST /api/remove body `{ip}` stops tracking a device (history retained on disk). POST /api/shutdown body `{}` exits application after response.

Demo launched separately with --demo; all scan/poll/config mutations disabled except monitor, label; no demo/real data mixing. Demo may use 24h generated sample fixture with conspicuous persistent banner “演示数据 · 未连接真实矿机”. No demo mode switch in real UI required; Windows package will include demo launch cmd.

Fleet history reflects actual known machines at sampling time; partial known readings must be labeled coverage, not implied full farm. Date formatting local browser timezone. Miner avg is firmware-provided average, not the app's 24h average. UI detail can compute arithmetic sample average explicitly marked “24h 有效采样均值”, show count / 288, gaps excluded. Fleet units can scale TH to PH; per miner TH/s. Temp is highest reported Celsius, label water values when present. Power sum is sum of online fresh reported readings only, with number/total coverage.
