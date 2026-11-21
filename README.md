# QBit Limit (Go + Gin)

限制 qBittorrent 每日、每月上行总流量以及上行速率，避免大上行流量被运营商限速/断网，同时兼顾 PT 保种与上传效率。

本版本使用 **Go + Gin** 重构，基于纯 Go SQLite (`modernc.org/sqlite`) 与 `embed.FS` 将全部前端页面（HTML/CSS/JS）打包进单个可执行文件，**无需 CGO，开箱支持多平台交叉编译**，并完整兼容原 Python 版本的 `qb.db` 历史数据。

## 核心特性

- **单文件部署**：前端 HTML/CSS/JS 全部通过 `//go:embed` 内嵌，无需额外静态目录或外部 CDN。
- **后台常规配置管理**：在 Web 控制台直接配置 qBittorrent 地址/账号/密码（支持保存前**测试连接**）、日/月上行限额、常规限速、超限熔断速率、账单重置日、轮询间隔等，保存后**即时热生效**。
- **深色 / 浅色主题切换**：支持现代清爽浅色与深色模式一键切换，适配桌面端与移动端。
- **可选管理密码保护**：默认关闭（局域网打开即用），可在「系统配置」中随时按需开启。
- **兼容原 `qb.db`**：直接指定原 Python 项目的 `qb.db` 即可无损读取历史记录，并新增 **qBittorrent 重启重置保护**（重启计数归零时不丢失当日已统计流量）。
- **多平台交叉编译**：纯 Go 无 CGO，一条命令生成 Linux (amd64 / arm64 / armv7)、macOS、Windows 可执行文件。

## 快速运行

```bash
# 直接运行（默认监听 0.0.0.0:8091，数据库使用当前目录或程序同目录 qb.db）
go run .

# 指定端口与原 Python 项目的历史 qb.db
./qbit_limit -port 8091 -db /path/to/qb.db

# 命令行查看近 30 天流量日志（替代原 logs.py）
./qbit_limit -db /path/to/qb.db -logs
```

启动后访问 `http://localhost:8091`，前往左侧 **「系统配置」** 填写 qBittorrent 连接信息与限速策略即可。

## 多平台交叉编译

```bash
# 编译当前平台
make build

# 一键交叉编译全部平台（产物输出至 dist/ 目录）
make build-all

# 或单独编译指定平台
make linux-amd64
make linux-arm64
make linux-armv7
make windows-amd64
```
