package taskobserver

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	shardSize  = 500
	windowSize = 1000
)

// RingLogger 是核心的日志收集器：
//   - 实现 io.Writer，可接入 log.SetOutput / slog
//   - 内存滑动窗口保留最近 windowSize 行用于页面展示
//   - 每积累 shardSize 行异步压缩上传到 COS
type RingLogger struct {
	mu       sync.Mutex
	taskName string
	safeName string
	runDir   string // tasks/<safeName>/logs/<runID>

	buf      []string // 滑动窗口
	pending  []string // 待上传缓冲
	shardIdx int
	shardURLs []string

	cos    *cosClient
	extra  io.Writer // 同时写到这里（如 os.Stderr）
}

func newRingLogger(taskName string, extra io.Writer) *RingLogger {
	safe := strings.NewReplacer(" ", "_", "/", "-", ":", "-").Replace(taskName)
	runID := time.Now().Format("20060102-150405")
	return &RingLogger{
		taskName: taskName,
		safeName: safe,
		runDir:   fmt.Sprintf("tasks/%s/logs/%s", safe, runID),
		extra:    extra,
	}
}

// setCOS 在 Observer 初始化后注入 COS 客户端（避免循环依赖）
func (r *RingLogger) setCOS(c *cosClient) {
	r.mu.Lock()
	r.cos = c
	r.mu.Unlock()
}

// Write 实现 io.Writer。
func (r *RingLogger) Write(p []byte) (int, error) {
	if r.extra != nil {
		r.extra.Write(p) //nolint:errcheck
	}
	text := strings.TrimRight(string(p), "\n")
	if text == "" {
		return len(p), nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.buf = append(r.buf, text)
	if len(r.buf) > windowSize {
		r.buf = r.buf[len(r.buf)-windowSize:]
	}
	r.pending = append(r.pending, text)
	if len(r.pending) >= shardSize {
		r.uploadShard()
	}
	return len(p), nil
}

// uploadShard 异步压缩上传当前 pending（调用方需持锁）。
func (r *RingLogger) uploadShard() {
	if len(r.pending) == 0 || r.cos == nil {
		return
	}
	r.shardIdx++
	key := fmt.Sprintf("%s/part-%04d.log.gz", r.runDir, r.shardIdx)
	lines := make([]string, len(r.pending))
	copy(lines, r.pending)
	r.pending = r.pending[:0]

	c := r.cos
	go func() {
		if _, err := c.putGzip(key, lines); err != nil {
			fmt.Fprintf(os.Stderr, "taskobserver: shard upload: %v\n", err)
			return
		}
		cosURL := fmt.Sprintf("https://%s.cos.%s.myqcloud.com/%s", c.bucket, c.region, key)
		r.mu.Lock()
		r.shardURLs = append(r.shardURLs, cosURL)
		r.mu.Unlock()
	}()
}

// flush 上传剩余不足 shardSize 的行（任务完成时调用）。
func (r *RingLogger) flush() {
	r.mu.Lock()
	r.uploadShard()
	r.mu.Unlock()
	time.Sleep(600 * time.Millisecond) // 等异步 goroutine 完成
}

func (r *RingLogger) window() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]string, len(r.buf))
	copy(cp, r.buf)
	return cp
}

func (r *RingLogger) shards() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.shardURLs...)
}

// newSlogLogger 返回写入 RingLogger 的标准 slog.Logger。
func newSlogLogger(w io.Writer) *slog.Logger {
	h := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: false,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(a.Value.Time().Format("2006-01-02T15:04:05.000Z07:00"))
			}
			return a
		},
	})
	return slog.New(h)
}
