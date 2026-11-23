# BTC scanning tool V1.2

Windows 10/11 x64 原生矿机监控工具。支持扫描 Antminer、Avalon 和 WhatsMiner，查看矿机状态、算力、各板温度和功率，汇总最近 24 小时算力，并从北京时间每天 08:00 起累计配对净收益。

[下载 GitHub 已发布的 V1.0 Windows x64 签名版](https://github.com/dkz9mfcfhn-oss/btc-scanning-tool/releases/download/V1.0/BTC-scanning-tool-V1.0.exe) · [V1.0 发布页](https://github.com/dkz9mfcfhn-oss/btc-scanning-tool/releases/tag/V1.0)

- 在 Windows 双击运行 EXE，配置矿场 IP 网段后选择「保存并扫描」。
- 使用 `BTC-scanning-tool-V1.2.exe -demo` 查看演示数据。
- 主界面「软件来源」可打开本项目仓库。
- 详细功能、历史版本和构建说明见 [Windows 使用与构建说明](README-NATIVE.md)。

## 构建

使用 Go 1.24 或更新版本。在 PowerShell 中执行：

```powershell
$env:CGO_ENABLED = '0'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
go build -trimpath -ldflags='-s -w -H windowsgui' -o BTC-scanning-tool-V1.2.exe .
```

仓库中包含源码、图标、构建资源、测试及可选的云端看护服务。第三方组件说明见 [THIRD-PARTY-NOTICES.txt](THIRD-PARTY-NOTICES.txt)。

## 版本规则

每次交付改动后的 EXE，小版本号加一。小版本从 `.0` 到 `.9`，随后主版本加一，从 `.0` 重新开始，例如 `V1.8`、`V1.9`、`V2.0`。更新时同步 `branding.go`、Windows 版本资源和 `BTC-scanning-tool-V主版本.小版本.exe` 文件名，并重新签名和生成 SHA-256 校验值。
