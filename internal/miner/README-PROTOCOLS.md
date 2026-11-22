# 只读协议适配说明

目标型号为 Antminer S19、Whatsminer M63S、Avalon 1566HA。本包依照公开厂商协议实现通用适配；尚未连接用户这三款实体矿机，不能把测试报文通过视为型号/固件实机验收。

## 数据入口

* Antminer：TCP 4028 的 `summary/stats/devdetails/version`；HTTP GET `/cgi-bin/stats.cgi`、`/cgi-bin/summary.cgi`、`/cgi-bin/get_miner_status.cgi`、`/cgi-bin/miner_type.cgi`。HTTP 支持 Digest MD5、MD5-sess、SHA-256、SHA-256-sess 和 qop=auth。先处理挑战再发送摘要，不尝试默认密码。现代字段 `rate_unit` 明确单位，老版 `MHS 5s` / `GHS 5s` 按字段单位换算为 TH/s。旧 `GHS5s/GHSav` 为 GH/s。未知单位留空。
* Whatsminer：旧 API TCP 4028 的 `summary`（`cmd`）；新 API TCP 4433 的 `get.miner.status`（`param: summary+pools+edevs`）及 `get.device.info`。新协议请求/响应均 4 字节小端长度前缀，处理零长度心跳、碎片读取、2 MiB 响应上限。`hash-realtime` 优先，缺失时使用 `hash-1min` 并标注来源；平均值取 `hash-average`。新 API 算力单位为 TH/s，旧 API `MHS` 与 `HS RT` 使用 MH/s。
* Avalon：TCP 4028 `summary/stats`，必要时 `estats`。支持 JSON 和传统管道分隔响应，解析 `MM ID*` 中的 `GHSmm/GHSavg/TMax/TAvg/Temp/PS`。同一设备命令顺序执行。1566HA 延续的实际字段和固件差异仍需现场确认，未伪造型号参数。

## 指标语义

* 实时值是采集时矿机接口报告的短期算力，曲线为每 5 分钟采样的快照；不是这 5 分钟的积分平均。
* “平均算力”使用设备自身平均字段，各品牌窗口可能不同，不等同本工具记录的 24 小时平均。
* 温度优先芯片最高温；缺失时使用明确的板温/矿机温度字段。`inletC/outletC` 仅接受名称明确的进水/出水温度字段，环境温度与进风温度不会冒充水温。未提供这些字段的固件会显示未上报。
* S19 固件不一定提供功率。缺失保持 null，不填额定值。`watt`、Whatsminer `Power`、`power-realtime` 或 PSU `pin` 按固件原值记录；功率设定/限制不当成实际值。
* 当前目标为用户确认的 1566HA 固件：PS[...] 末项就是功率，按末项 W 解析并显示来源。该约定针对本项目目标固件；其他型号/固件可能采用不同布局。日志中多个历史快照取最新，多个 MM 模块按模块求和。
* Avalon `GHSmm` 是芯片原始/校准前算力，与矿池有效算力可能不同，明确标注。多 MM 模块按模块求和。
* 0 是有效值，缺失为 null；网络失败由上层保存为离线样本。

## 限制与安全边界

仅允许 IPv4 字面地址；每次连接 1.5 秒拨号上限、3 秒 I/O 上限，并服从调用者取消与整体期限。每个响应最大 2 MiB。HTTP 不使用系统代理，不跟随重定向。所有命令均读取状态；不重启、不改池、不改功率、不刷固件。

## 官方依据（2026-10-28 核对）

* [BITMAIN Antminer API](https://docs.bitmain.com/en/antminer/CONTENTS.html) — HTTP 只读接口、Digest、rate_unit、芯片温度、watt。
* [Whatsminer API v3](https://apidoc.whatsminer.com/) — TCP 4433、小端帧、只读 get 命令、TH/s、实时功率和设备信息。
* [Whatsminer API 手册入口](https://support.whatsminer.com/en-US/article/489?title=WhatsMiner+API_User%27s+Manual) — 厂商维护的版本入口。旧 V2.0.4 PDF 在搜索索引仍有协议报文，原 PDF URL 当日直接读取返回 404，故旧版实现另以标准报文测试验证，未声称现行下载链接可用。
* [Canaan Avalon A10 API 官方文档](https://github.com/Canaan-Creative/avalon10-docs/blob/master/Universal%20API/Avalon%20A10%20API%E6%89%8B%E5%86%8C-CN.md) — 4028、顺序连接、stats/estats、PS 输出功率。此文档是协议家族依据，不是 1566HA 专用适配认证。

## 2.3 Byteon REST兼容适配

新增 `/api/v1/info` 识别和 `/api/v1/summary` 读取，解析 `miner.power_consumption` / `power_usage`、`hr_realtime/hr_average`、`chip_temp.max`。summary认证失败时 POST `/api/v1/unlock`，以 `pw` 换取 `token`，临时使用Bearer。未知固件标识不发送密码，所有请求拒绝重定向。

依据来自Vnish官方FAQ实际链接的 Anthill Miner Dash API，commit fbffafa0fada248e552e23392bbf6e0e746009c9。实际读取 paths/unlock.yaml、paths/summary.yaml、components/schemas/info.yaml、miner_stats.yaml、password.yaml。Byteon官网确认仪表盘有功耗详情，但未找到独立的Byteon API契约。本实现属于明确标记的兼容适配，未实机验证。

## 2.4 读取流程与现场诊断

信息接口401/403时，先通过公开summary标识或首页title确认兼容固件，再以已有密码认证并重读info和summary。支持明确的data/result/info/summary封装，不猜测未知算力单位。REST未提供功率也允许CGI补齐，保留原固件身份。TCP识别/Whatsminer v3/REST分别有阶段期限，受单机总期限约束。

`Diagnose`仅请求固定状态端点及必要的unlock认证，输出字段白名单脱敏JSON；不保存原始响应、令牌、密码、矿池、用户、钱包或原始日志。GUI与常规采集互斥。新模拟测试覆盖信息接口认证、封装、功率补读、错误分类和脱敏；未获得现场Byteon响应，仍不能保证全部Byteon版本兼容。

软件默认5分钟采样，现可选30秒至30分钟；历史记录保留各样本当时的采样间隔。

## 2.5 分板温度与简洁显示

新增共享telemetry模型，按原始板号存储每板Chip/PCB平均、最低、最高、完整传感器槽位、其他温度和来源。缺失槽位为null；无效温度（非有限数、低于-50或高于200摄氏度）不参与均值。真实0保留。固件均温averageKind=reported；至少两个有效点才生成sensor_mean，并在主界面加星号。不会对极值取中点，不会把单点当整板均温，也不把整机平均分摊各板。

Antminer官方stats.chain[].index/temp_chip/temp_pcb/temp_pic为原始依据。旧API的temp2_N等保留链编号；未确认含义的temp3_N/temperatureN作为独立命名温度保留。
Whatsminer v3 msg.edevs[].id/slot以及chip-temp-avg/min/max用于每板数据；旧版DEVS[].ASC/Slot和Chip Temp Avg/Min/Max兼容。旧Temperature是板出风口温度。summary.board-temperature[]未确认缺板后槽位映射，保留为独立序列。info.power.temp0是电源温度。
Avalon MTavg/MTmax分别为每板芯片平均/最高，PVT_Tn为板n的芯片温度序列；按MM模块和板索引区分。此依据来自A10/A11/A12日志指南，不代表1566HA现场验收。
Byteon兼容Anthill REST的miner.chains[].id/chip_temp_sens/pcb_temp_sens：temp_sensor.status只取measure，init/error/unknown保留空位。temp_minmax只有min/max时绝不造平均。

优先显示数据较完整的一组接口板记录，全部来源保留在展开详情和历史/CSV中。不会凭相同数字板号跨协议合板。读取流程在没有板均温时会继续尝试CGI、Whatsminer edevs或Avalon estats。诊断追加温度结论与解析出的板温数据，保留原始数值字段并继续移除敏感字段。

本轮核对的primary sources：
- https://docs.bitmain.com/en/antminer/CONTENTS.html （stats链温度）
- https://apidoc.whatsminer.com/ （v3 edevs与PSU温度）
- https://www.whatsminer.com/file/WhatsminerAPI%20V2.0.4.pdf （旧版官方搜索缓存，直接URL当前404）
- https://static.canaan.io/uploads/supports_file/1_Miner_Log_Technical_Guidance.pdf （第5页MTavg/MTmax/PVT_T说明，适用范围A10/A11/A12）
- https://api.bitbucket.org/2.0/repositories/anthill-farm/miner-dash-api/src/develop/components/schemas/miner_stats/chain.yaml
- https://api.bitbucket.org/2.0/repositories/anthill-farm/miner-dash-api/src/develop/components/schemas/miner_stats/temp_sensor.yaml
- https://api.bitbucket.org/2.0/repositories/anthill-farm/miner-dash-api/src/develop/components/schemas/miner_stats/temp_minmax.yaml

## 无算力原因与可确认范围

只以本次有效实时值为 0 判定无算力并触发日志补读。未上报、采集失败和正算力不会触发故障日志请求。常规状态、温度及算力的补采仍可用于完善遥测。恢复时清除故障依据，过期采样显示待更新。

Antminer SUMMARY[].status[] 的 type/status/code/msg 用于当前异常分类，STATS[].miner-mode=1 用于休眠状态。原厂固件在明确零算力时只读 [当前内核日志](https://docs.bitmain.com/en/antminer/CONTENTS.html#get-miner-log-data)。仅识别官方故障示例中明确的温度、风扇、电源、PIC、算力板芯片信号，归为待确认线索。检测到后续新轮次或恢复标记时舍弃更早的候选。API 外层 STATUS 是请求结果。
Whatsminer v3 get.miner.status读取summary+pools+edevs，get.device.info读取miner.working及error-code。error-code条目的有效期未明确，合法数字码和时间保留但标为possible，不自动解释为当前硬件损坏。API顶层code是请求结果。
Avalon 的 SYSTEMSTATU、ECHU、ECMM 用于核对当前状态。零算力时从已读的统计、FMS 或后台日志提取 BOOTBY，缺该字段时尝试短时补读。详情和 Bark 单列显示完整的「上次重启记录」。[Canaan 的日志指南](https://static.canaan.io/uploads/supports_file/1_Miner_Log_Technical_Guidance.pdf)把 BOOTBY 定义为上次重启原因，适用范围为 A10/A11/A12。1566HA 的带点十六进制值保留原码，具体含义待对应固件确认。
Byteon 兼容 REST 的 miner.miner_status.miner_state 及 chains[].status 用于整机和算力板状态。明确零算力时，已确认 Byteon 或 Anthill 的设备可通过只读 [矿机日志接口](https://api.bitbucket.org/2.0/repositories/anthill-farm/miner-dash-api/src/fbffafa0fada248e552e23392bbf6e0e746009c9/paths/logs/%7Blog_type%7D/get.yaml)补充线索。复用已取得的会话，要求可核对的近期时间戳和已知故障标记。保留安全故障类别，原始日志不进入诊断和通知。

仅所有已返回且明确的矿池状态均 Dead 时提示可能的矿池连接问题，Alive 或未知状态会阻止这个推断。温度高、单个风扇/芯片缺值、累积 HW 错误不单独推断整机零算力原因。无充分依据时显示原因未确认。

结构化依据随历史和CSV保存，诊断schemaVersion=2增加measurements.health。详情白名单显示代码、时间、板号与状态；新诊断可能发生在原采样之后，应以各自时间为准。无重启、改矿池、调功率等控制命令。

核对依据：BITMAIN官方Antminer接口目录，Whatsminer官方v3文档，Canaan官方API/日志指南及Anthill Miner Dash API公开schema（与前文链接相同）。均为协议依据，不等于这三种用户实机验收。

## 2.7 份额、全场趋势及移除功率显示

CGMiner SUMMARY.Accepted/Rejected、Antminer POOLS.accepted/rejected、Byteon兼容miner.pools[].accepted/rejected均以完整、非负整数对读取，不混合不同端点的单边计数，也不丢弃缺失矿池后假称完整。在线正常时同样按需读取pools。已确认的神马v3 summary.power-on-stats.reject-percent可单机显示，但没有份额数量不能进入全场加权；旧4028若有计数仍可使用。

核对来源：
- https://docs.bitmain.com/en/antminer/CONTENTS.html （pools字段）
- https://apidoc.whatsminer.com/ （get.miner.status的power-on-stats与pools，2026-10-31核对官网JS中公开文档）
- https://github.com/Canaan-Creative/avalon10-docs/blob/master/Universal%20API/Avalon%20A10%20API%20manual-EN.md
- https://api.bitbucket.org/2.0/repositories/anthill-farm/miner-dash-api/src/develop/components/schemas/miner_stats/pool.yaml

所有新样本不再保存功率字段值，Windows界面、汇总、CSV和采集诊断移除功率展示。部分内部解析字段保留以兼容旧记录和识别固件；不会触发设备功率控制。电源故障原因和电源温度仍属于健康/温度信息。
