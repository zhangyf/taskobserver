// cmd/main.go — taskobserver 独立运行示例，同时也是 package 用法的参考。
package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"taskobserver"
)

func main() {
	cfg := taskobserver.ConfigFromEnv()
	// 命令行参数覆盖
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--bucket":
			if i+1 < len(args) { cfg.Bucket = args[i+1]; i++ }
		case "--region":
			if i+1 < len(args) { cfg.Region = args[i+1]; i++ }
		case "--secret-id":
			if i+1 < len(args) { cfg.SecretID = args[i+1]; i++ }
		case "--secret-key":
			if i+1 < len(args) { cfg.SecretKey = args[i+1]; i++ }
		case "--base-url":
			if i+1 < len(args) { cfg.BaseURL = args[i+1]; i++ }
		case "--task":
			if i+1 < len(args) { cfg.TaskName = args[i+1]; i++ }
		}
	}
	cfg.ExtraWriter = os.Stderr
	if cfg.Interval == 0 {
		cfg.Interval = 3 * time.Second
	}

	obs, err := taskobserver.NewWithError(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage: taskobserver [flags]")
		fmt.Fprintln(os.Stderr, "  --bucket      <name>    (or TASKOBS_BUCKET)")
		fmt.Fprintln(os.Stderr, "  --region      <region>  (or TASKOBS_REGION)")
		fmt.Fprintln(os.Stderr, "  --secret-id   <id>      (or TASKOBS_SECRET_ID)")
		fmt.Fprintln(os.Stderr, "  --secret-key  <key>     (or TASKOBS_SECRET_KEY)")
		fmt.Fprintln(os.Stderr, "  --base-url    <url>     (or TASKOBS_BASE_URL, optional)")
		fmt.Fprintln(os.Stderr, "  --task        <name>    (or TASKOBS_TASK, optional)")
		os.Exit(1)
	}

	// 把标准库 log 接入 observer
	log.SetOutput(obs.Writer())
	log.SetFlags(0) // slog 自带时间，不需要 log 前缀

	total := 10
	current := 0

	obs.Start(func() (int, int) { return current, total })
	fmt.Printf("Overview : %s\n", obs.OverviewURL())
	fmt.Printf("Task page: %s\n", obs.TaskURL())

	// —— 模拟任务 ——
	logger := obs.NewSlogLogger()
	logger.Info("task started", "total", total)
	for i := 1; i <= total; i++ {
		current = i
		logger.Info("processing", "step", i, "total", total)
		switch i {
		case 4:
			logger.Warn("slow response from upstream", "attempt", 2)
		case 7:
			logger.Debug("cache miss, fetching from origin")
		case 9:
			logger.Error("transient error", "err", "connection reset", "will_retry", true)
		}
		time.Sleep(2 * time.Second)
	}
	logger.Info("task completed successfully")
	obs.Done()
	fmt.Println("Done.")
}
