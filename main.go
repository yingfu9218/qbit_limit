package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/tabwriter"
	"time"

	"qbit_limit/internal/config"
	"qbit_limit/internal/db"
	"qbit_limit/internal/monitor"
	"qbit_limit/internal/web"
)

var (
	version   = "2.0.0"
	buildTime = "dev"
)

func main() {
	var (
		port        = flag.Int("port", 8091, "Web 服务监听端口")
		host        = flag.String("host", "0.0.0.0", "Web 服务监听地址")
		dbPath      = flag.String("db", "", "SQLite 数据库路径（默认程序同目录 qb.db）")
		showLogs    = flag.Bool("logs", false, "命令行打印近30天流量记录后退出（替代原 logs.py）")
		showVersion = flag.Bool("version", false, "打印版本信息")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("qbit_limit %s (build: %s)\n", version, buildTime)
		return
	}

	resolvedDB := *dbPath
	if resolvedDB == "" {
		if exe, err := os.Executable(); err == nil {
			// If running via `go run`, fall back to current working directory
			dir := filepath.Dir(exe)
			if filepath.Base(dir) == "exe" || os.Getenv("QBIT_LIMIT_CWD_DB") == "1" {
				resolvedDB = "qb.db"
			} else {
				resolvedDB = filepath.Join(dir, "qb.db")
			}
		} else {
			resolvedDB = "qb.db"
		}
		// Prefer existing ./qb.db in cwd if present
		if _, err := os.Stat("qb.db"); err == nil {
			resolvedDB = "qb.db"
		}
	}

	store, err := db.Open(resolvedDB)
	if err != nil {
		log.Fatalf("初始化数据库失败 (%s): %v", resolvedDB, err)
	}
	defer store.Close()

	if *showLogs {
		printCLILogs(store)
		return
	}

	cfg, err := store.LoadSettings()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	mon := monitor.NewService(store, cfg)
	mon.Start()
	defer mon.Stop()

	srv := web.NewServer(store, mon)
	addr := fmt.Sprintf("%s:%d", *host, *port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("QBit Limit v%s 启动成功", version)
		log.Printf("数据库路径: %s", resolvedDB)
		log.Printf("Web 控制台: http://%s", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Web 服务启动失败: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("正在优雅关闭服务...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	log.Println("服务已退出")
}

func printCLILogs(store *db.Store) {
	logs, err := store.ListRecentLogs(30, false)
	if err != nil {
		log.Fatalf("读取日志失败: %v", err)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "日期\t上传\t下载\t是否触发限速\t限速原因\t创建时间\t更新时间")
	for _, r := range logs {
		lim := "否"
		if r.DenyLimit == 1 {
			lim = "是"
		}
		reason := r.LimitReason
		if reason == "" {
			reason = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.DayTime,
			config.FormatBytes(r.Upload),
			config.FormatBytes(r.Down),
			lim,
			reason,
			r.CreatedAt,
			r.UpdatedAt,
		)
	}
	_ = w.Flush()

	now := time.Now()
	monthStart := now.Format("2006-01") + "-01"
	today := now.Format("2006-01-02")
	sum, err := store.GetPeriodSummary(monthStart, today)
	if err == nil {
		fmt.Printf("\n当月总上行流量: %s\n", config.FormatBytes(sum.TotalUpload))
		fmt.Printf("当月总下行流量: %s\n", config.FormatBytes(sum.TotalDownload))
		fmt.Printf("当月触发限速天数: %d 天\n", sum.LimitedDays)
	}
}
