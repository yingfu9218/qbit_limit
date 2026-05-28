# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

qBittorrent 上行流量/速率守门员。后台进程每 10 秒轮询 qBittorrent，把每日累计上传写入 SQLite，达到日/月阈值时把 qBittorrent 的全局上行速率压到 `denyUploadSpeedLimit`（默认 5 KB/s）实现"软禁上传"，跨日/跨月自动恢复。同进程内还跑一个 Flask Web UI（端口 8091）展示最近 30 天数据。

## Commands

```bash
# 安装依赖
pip3 install -r requirements.txt

# 首次运行前必须复制并修改配置（config.py 在 .gitignore 中，不会被提交）
cp config.example.py config.py
# 编辑 config.py 填写 qBittorrent host/账号、日/月流量与速率阈值

# 启动主程序（同时启动调度器 + Flask Web UI on 0.0.0.0:8091）
python3 qbit_limit.py

# 命令行查看最近 30 天日志
python3 logs.py
```

部署到生产用 supervisor 守护 `qbit_limit.py`（`supervisorctl restart qbit_limit`）。`log.txt` 里记录了开发机 → 服务器的 `scp` 同步命令，作者通常本地改完用 scp 推到服务器再重启 supervisor。

## Architecture

代码量小（核心 ~230 行），但有几条不容易从单文件看出的约定：

**单进程双角色**：`qbit_limit.py` 的 `__main__` 同时承载两个职责——主线程跑 `schedule` 循环（每 10 秒 `updateLog()`），守护线程跑 Flask（`start_web_service`，`use_reloader=False`，必须关闭 reloader 否则会 fork 出第二个调度器）。两边共享同一个 `sqlite3` 连接，靠 `check_same_thread=False` 放行跨线程访问。

**累计量 vs 增量的差分逻辑**：qBittorrent `transfer.info` 返回的 `up_info_data` / `dl_info_data` 是**进程启动以来的累计字节**，不是当日量。`log` 表第一次写入当日记录时把当时的累计值存进 `total_upload_first` / `total_download_first` 作为基线，之后每次 `upload = total_upload - total_upload_first`。**重要**：如果 qBittorrent 被重启，累计值会从 0 重新开始，代码用 `if total_upload >= row[2]` 判断这种"基线被回退"情况并重写基线（见 `qbit_limit.py:114-122`），改这段逻辑时务必保留该重置分支。

**两种触发限速的路径**：
- 当月累计超 `uploadLimitEveryMonth` → `isMonthUploadOver()` 在每次 `updateLog` 开头检查
- 当天增量超 `uploadLimitEveryDay` → 在差分计算之后检查

两条路径都通过 `denyUpload()` 把全局上行速率设为 `denyUploadSpeedLimit`，而 `denyUpload` 只在当前限速 ≠ 目标值时才发请求（避免每 10 秒重复调用 qBittorrent API）。恢复正常限速也走同样的"幂等"判断（`qbit_limit.py:132-135`）。日切/月切的"自动恢复"靠 `day_time` 换天后走 INSERT 新行分支自然实现，没有显式的解禁定时器。

**配置即代码**：`config.py` 是一个 Python 类（不是 .env / yaml），导入即生效。`config.example.py` 是模板，真正的 `config.py` 含明文密码所以被 gitignore。修改阈值改 `config.py` 即可，单位都是字节（注释里有 `20*1024*1024*1024` 这种内联换算）。

**前端是预构建的静态产物**：`frontend/dist/` 下的 `index.html` / `app.js` / `styles.css` 是手写的、直接 commit 的静态文件——仓库里没有 `frontend/src/` 或 build 工具链。改 UI 直接编辑 `dist/` 即可。前端每 60 秒 `setInterval(init, 60000)` 拉一次 `/refresh_data`。Flask 用 `send_from_directory` 提供静态文件，`index.html` 引用 `app.js?v=3` 这种手动版本号刷缓存。

**`logs.py` 是独立 CLI**：用 `prettytable` 在终端打印同样的 30 天数据，和 Web UI 完全独立，共享的只有 `qb.db` 这个文件。改表结构要同步两边的 SELECT。

## Schema

```sql
CREATE TABLE log (
  id integer PRIMARY KEY AUTOINCREMENT,
  day_time DATE,               -- 'YYYY-MM-DD'，每天一行
  total_upload_first integer,   -- 当日首次记录时 qBittorrent 累计上传值（基线）
  total_download_first integer, -- 同上，下载
  upload integer,               -- 当日增量上传 = total_upload - total_upload_first
  down integer,                 -- 当日增量下载
  deny_limit integer DEFAULT 0, -- 1 表示当天触发过限速
  created_at DATE,
  updated_at DATE
);
```

SQL 全部用 `%` 字符串拼接（没有参数化），日期/数字来自系统时间和 qBittorrent API 所以注入面很小，但新增字段时维持同风格即可。

## Gotchas

- `qb.db` 被 commit 进了仓库（开发机用的样本数据），`git status` 经常显示它 modified——除非确实要更新样本数据，否则不要把它加进提交。
- `setUploadLimit()` 在 `__main__` 启动时会**立刻**调一次 qBittorrent API；本地起服务前确认 `config.py` 的 host 是开发用实例而不是生产。
- Flask 跑在 `0.0.0.0:8091` 没有任何鉴权，部署时假定在内网或反代后面。
