// Package taskobserver 提供将任务日志和进度实时推送到 COS 静态页面的能力。
//
// 基本用法：
//
//	obs := taskobserver.New(taskobserver.Config{
//	    Bucket:    "my-bucket",
//	    Region:    "ap-nanjing",
//	    SecretID:  "AKIDxxx",
//	    SecretKey: "xxxxx",
//	    TaskName:  "数据迁移",
//	})
//	log.SetOutput(obs.Writer())
//	obs.Start(func() (int, int) { return current, total })
//	defer obs.Done()
package taskobserver

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Config 是 Observer 的全部配置，零值之外的字段均为必填。
type Config struct {
	// COS 存储配置（必填）
	Bucket    string
	Region    string
	SecretID  string
	SecretKey string

	// BaseURL 自定义域名，留空自动使用 COS 原始域名
	BaseURL string

	// TaskName 任务名称，显示在页面标题和总览表格中（必填）
	TaskName string

	// Interval 页面推送间隔，默认 5s
	Interval time.Duration

	// ExtraWriter 日志同时写到哪里，传 nil 则静默，传 os.Stderr 则打印到终端
	ExtraWriter io.Writer
}

func (c *Config) fillDefaults() error {
	if c.Bucket == "" || c.Region == "" {
		return fmt.Errorf("taskobserver: Bucket and Region are required")
	}
	if c.SecretID == "" || c.SecretKey == "" {
		return fmt.Errorf("taskobserver: SecretID and SecretKey are required")
	}
	if c.TaskName == "" {
		return fmt.Errorf("taskobserver: TaskName is required")
	}
	if c.Interval <= 0 {
		c.Interval = 5 * time.Second
	}
	if c.BaseURL == "" {
		c.BaseURL = fmt.Sprintf("https://%s.cos.%s.myqcloud.com", c.Bucket, c.Region)
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	return nil
}

// Observer 是核心对象，持有 RingLogger、COS 客户端和推送循环。
type Observer struct {
	cfg        Config
	cos        *cosClient
	rl         *RingLogger
	progressFn func() (int, int)
	doneCh     chan struct{}
	startTime  string
}

// New 创建一个 Observer，失败（配置不合法）时 panic。
// 如需处理错误请用 NewWithError。
func New(cfg Config) *Observer {
	obs, err := NewWithError(cfg)
	if err != nil {
		panic(err)
	}
	return obs
}

// NewWithError 创建 Observer，返回错误而不是 panic。
func NewWithError(cfg Config) (*Observer, error) {
	if err := cfg.fillDefaults(); err != nil {
		return nil, err
	}
	c := newCOSClient(cfg.Bucket, cfg.Region, cfg.SecretID, cfg.SecretKey)
	rl := newRingLogger(cfg.TaskName, cfg.ExtraWriter)
	return &Observer{
		cfg:    cfg,
		cos:    c,
		rl:     rl,
		doneCh: make(chan struct{}),
	}, nil
}

// Writer 返回一个 io.Writer，可直接传给 log.SetOutput 或 slog.NewTextHandler。
// 所有写入内容会进入滑动窗口并按分片上传到 COS。
func (o *Observer) Writer() io.Writer {
	return o.rl
}

// NewSlogLogger 返回一个标准格式的 slog.Logger，直接写入 Observer。
func (o *Observer) NewSlogLogger() *slog.Logger {
	return newSlogLogger(o.rl)
}

// Start 启动后台推送循环。
// progressFn 每次推送前调用，返回 (current, total)；传 nil 则进度条不动。
// Start 是非阻塞的，立即返回。
func (o *Observer) Start(progressFn func() (int, int)) {
	o.progressFn = progressFn
	o.startTime = time.Now().Format("2006-01-02 15:04:05")
	// 注册任务到总览页
	upsertRegistry(o.cos, o.cfg.BaseURL, taskMeta{
		Name:      o.cfg.TaskName,
		SafeName:  o.rl.safeName,
		Status:    "running",
		Progress:  0,
		StartTime: o.startTime,
		PageURL:   o.taskPageURL(),
	})
	uploadIndexPage(o.cos, o.cfg.BaseURL)

	go o.watchLoop()
}

// Done 标记任务完成：上传剩余分片、最终推送页面（状态=Completed）、更新总览。
// Done 会阻塞直到最终页面上传完毕。
func (o *Observer) Done() {
	close(o.doneCh)
	// 留足时间让 watchLoop goroutine 完成最终推送
	time.Sleep(200 * time.Millisecond)
}

// Log 直接写一行日志（level=INFO）。
func (o *Observer) Log(msg string, args ...any) {
	o.NewSlogLogger().Info(msg, args...)
}

// OverviewURL 返回总览页地址。
func (o *Observer) OverviewURL() string {
	return o.cfg.BaseURL + "/index.html"
}

// TaskURL 返回当前任务详情页地址。
func (o *Observer) TaskURL() string {
	return o.taskPageURL()
}

func (o *Observer) taskPageURL() string {
	return fmt.Sprintf("%s/tasks/%s/index.html", o.cfg.BaseURL, o.rl.safeName)
}

func (o *Observer) taskPageKey() string {
	return fmt.Sprintf("tasks/%s/index.html", o.rl.safeName)
}

func (o *Observer) watchLoop() {
	push := func(isDone bool) {
		cur, tot := 0, 0
		if o.progressFn != nil {
			cur, tot = o.progressFn()
		}
		pct := 0
		if tot > 0 {
			pct = cur * 100 / tot
		}
		if isDone {
			pct = 100
		}
		lines := o.rl.window()
		shards := o.rl.shards()
		page := buildTaskHTML(o.cfg.TaskName, lines, cur, tot, isDone, shards)
		if err := o.cos.putString(o.taskPageKey(), "text/html; charset=utf-8", page); err != nil {
			fmt.Fprintf(os.Stderr, "taskobserver: upload task page: %v\n", err)
		}
		status := "running"
		endTime := ""
		if isDone {
			status = "completed"
			endTime = time.Now().Format("2006-01-02 15:04:05")
		}
		upsertRegistry(o.cos, o.cfg.BaseURL, taskMeta{
			Name:      o.cfg.TaskName,
			SafeName:  o.rl.safeName,
			Status:    status,
			Progress:  pct,
			StartTime: o.startTime,
			EndTime:   endTime,
			PageURL:   o.taskPageURL(),
		})
		uploadIndexPage(o.cos, o.cfg.BaseURL)
	}

	for {
		select {
		case <-o.doneCh:
			o.rl.flush()
			push(true)
			return
		case <-time.After(o.cfg.Interval):
			push(false)
		}
	}
}

// ConfigFromEnv 从环境变量读取配置，方便不想硬编码的场景。
//
//	TASKOBS_BUCKET      必填
//	TASKOBS_REGION      必填
//	TASKOBS_SECRET_ID   必填
//	TASKOBS_SECRET_KEY  必填
//	TASKOBS_BASE_URL    选填
//	TASKOBS_TASK        选填（也可在代码里覆盖）
func ConfigFromEnv() Config {
	return Config{
		Bucket:    os.Getenv("TASKOBS_BUCKET"),
		Region:    os.Getenv("TASKOBS_REGION"),
		SecretID:  os.Getenv("TASKOBS_SECRET_ID"),
		SecretKey: os.Getenv("TASKOBS_SECRET_KEY"),
		BaseURL:   os.Getenv("TASKOBS_BASE_URL"),
		TaskName:  os.Getenv("TASKOBS_TASK"),
	}
}
