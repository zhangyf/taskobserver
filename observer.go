// Package taskobserver 提供将任务日志和进度实时推送到 COS 静态页面的能力。
//
// 基本用法：
//
//	obs := taskobserver.New(taskobserver.Config{...})
//	log.SetOutput(obs.Writer())
//	obs.Start(func() (int, int) { return current, total })
//	defer obs.Done()
package taskobserver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"objstore"
)

// Status 任务状态
type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusKilled    Status = "killed"
)

// HeartbeatTimeout 前端判断进程已死的超时阈值（秒），写入页面供 JS 使用
const HeartbeatTimeout = 120

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
		c.BaseURL = fmt.Sprintf("https://%s.cos-internal.%s.tencentcos.cn", c.Bucket, c.Region)
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	return nil
}

// Observer 是核心对象，持有 RingLogger、objstore.Store 和推送循环。
type Observer struct {
	cfg         Config
	store       objstore.Store
	rl          *RingLogger
	progressFn  func() (int, int)
	doneCh      chan struct{}
	finishedCh  chan struct{}
	finalStatus Status
	startTime   string
}

// New 创建一个 Observer，失败时 panic。
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

	store, err := objstore.New(objstore.Config{
		Provider:  objstore.ProviderCOS,
		Bucket:    cfg.Bucket,
		Region:    cfg.Region,
		SecretID:  cfg.SecretID,
		SecretKey: cfg.SecretKey,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 objstore 失败: %w", err)
	}

	rl := newRingLogger(cfg.TaskName, cfg.ExtraWriter)
	rl.setStore(store, cfg.Bucket, cfg.Region)

	return &Observer{
		cfg:         cfg,
		store:       store,
		rl:          rl,
		doneCh:      make(chan struct{}),
		finishedCh:  make(chan struct{}),
		finalStatus: StatusCompleted,
	}, nil
}

// Writer 返回一个 io.Writer，可直接传给 log.SetOutput 或 slog.NewTextHandler。
func (o *Observer) Writer() io.Writer {
	return o.rl
}

// NewSlogLogger 返回一个标准格式的 slog.Logger，直接写入 Observer。
func (o *Observer) NewSlogLogger() *slog.Logger {
	return newSlogLogger(o.rl)
}

// Start 启动后台推送循环，并注册信号处理（SIGTERM/SIGINT → killed）。
func (o *Observer) Start(progressFn func() (int, int)) {
	o.progressFn = progressFn
	o.startTime = time.Now().Format("2006-01-02 15:04:05")

	upsertRegistry(o.store, o.cfg.BaseURL, taskMeta{
		Name:          o.cfg.TaskName,
		SafeName:      o.rl.safeName,
		Status:        string(StatusRunning),
		Progress:      0,
		StartTime:     o.startTime,
		PageURL:       o.taskPageURL(),
		LastHeartbeat: time.Now().Unix(),
	})
	uploadIndexPage(o.store, o.cfg.BaseURL)

	go o.handleSignals()
	go o.watchLoop()
}

// Done 标记任务成功完成，阻塞直到最终页面上传完成。
func (o *Observer) Done() {
	o.finalStatus = StatusCompleted
	close(o.doneCh)
	<-o.finishedCh
}

// Fail 标记任务失败，阻塞直到最终页面上传完成。
func (o *Observer) Fail(err error) {
	if err != nil {
		o.Log("任务失败", "error", err.Error())
	}
	o.finalStatus = StatusFailed
	close(o.doneCh)
	<-o.finishedCh
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

// DeleteTask 删除任务（归档数据到指定路径）
func (o *Observer) DeleteTask(taskSafeName string, archivePath string) error {
	return deleteTask(o.store, o.cfg.Bucket, o.cfg.Region, o.cfg.BaseURL, taskSafeName, archivePath)
}

func (o *Observer) taskPageURL() string {
	return fmt.Sprintf("%s/tasks/%s/index.html", o.cfg.BaseURL, o.rl.safeName)
}

func (o *Observer) taskPageKey() string {
	return fmt.Sprintf("tasks/%s/index.html", o.rl.safeName)
}

func (o *Observer) handleSignals() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	sig := <-ch
	fmt.Fprintf(os.Stderr, "\ntaskobserver: received signal %v, marking task as killed\n", sig)
	o.Log("进程收到信号，任务标记为 killed", "signal", sig.String())
	o.finalStatus = StatusKilled
	select {
	case <-o.doneCh:
	default:
		close(o.doneCh)
	}
	<-o.finishedCh
	os.Exit(1)
}

func (o *Observer) watchLoop() {
	defer close(o.finishedCh)

	push := func(status Status) {
		cur, tot := 0, 0
		if o.progressFn != nil {
			cur, tot = o.progressFn()
		}
		pct := 0
		if tot > 0 {
			pct = cur * 100 / tot
		}
		if status != StatusRunning {
			pct = 100
		}
		if status == StatusKilled || status == StatusFailed {
			if tot > 0 {
				pct = cur * 100 / tot
			}
		}

		lines := o.rl.window()
		shards := o.rl.shards()
		page := buildTaskHTML(o.cfg.TaskName, lines, cur, tot, status, shards)
		if err := o.store.PutObject(context.Background(), o.taskPageKey(), []byte(page)); err != nil {
			fmt.Fprintf(os.Stderr, "taskobserver: upload task page: %v\n", err)
		}

		endTime := ""
		if status != StatusRunning {
			endTime = time.Now().Format("2006-01-02 15:04:05")
		}
		upsertRegistry(o.store, o.cfg.BaseURL, taskMeta{
			Name:          o.cfg.TaskName,
			SafeName:      o.rl.safeName,
			Status:        string(status),
			Progress:      pct,
			StartTime:     o.startTime,
			EndTime:       endTime,
			PageURL:       o.taskPageURL(),
			LastHeartbeat: time.Now().Unix(),
		})
		uploadIndexPage(o.store, o.cfg.BaseURL)
	}

	for {
		select {
		case <-o.doneCh:
			o.rl.flush()
			push(o.finalStatus)
			return
		case <-time.After(o.cfg.Interval):
			push(StatusRunning)
		}
	}
}

// ConfigFromEnv 从环境变量读取配置。
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

// deleteTask 删除任务并归档数据
func deleteTask(store objstore.Store, bucket, region, baseURL, taskSafeName, archivePath string) error {
	if archivePath == "" {
		archivePath = "archive"
	}
	archivePath = strings.TrimSuffix(archivePath, "/") + "/"

	// 1. 从注册表中移除任务
	registryMu.Lock()
	tasks := loadRegistry(store)
	var newTasks []taskMeta
	var found bool
	for _, task := range tasks {
		if task.SafeName == taskSafeName {
			found = true
			continue
		}
		newTasks = append(newTasks, task)
	}
	if !found {
		registryMu.Unlock()
		return fmt.Errorf("任务未找到: %s", taskSafeName)
	}
	if err := saveRegistry(store, newTasks); err != nil {
		registryMu.Unlock()
		return fmt.Errorf("保存注册表失败: %v", err)
	}
	registryMu.Unlock()

	// 2. 列出任务文件并归档
	ctx := context.Background()
	taskPrefix := "tasks/" + taskSafeName + "/"
	objects, err := store.ListObjects(ctx, taskPrefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "taskobserver: 列出任务文件失败: %v\n", err)
	} else {
		for _, key := range objects {
			data, err := store.GetAll(ctx, key)
			if err != nil {
				fmt.Fprintf(os.Stderr, "taskobserver: 读取文件失败 %s: %v\n", key, err)
				continue
			}
			destKey := archivePath + key
			if err := store.PutObject(ctx, destKey, data); err != nil {
				fmt.Fprintf(os.Stderr, "taskobserver: 归档失败 %s -> %s: %v\n", key, destKey, err)
				continue
			}
			fmt.Fprintf(os.Stderr, "taskobserver: 已归档: %s -> %s\n", key, destKey)
		}
	}

	// 3. 创建归档标记
	marker := archivePath + taskPrefix + "deleted_at.txt"
	content := fmt.Sprintf("归档时间: %s\n原位置: %s\n归档路径: %s\n",
		time.Now().Format("2006-01-02 15:04:05"), taskPrefix, archivePath)
	store.PutObject(ctx, marker, []byte(content)) //nolint:errcheck

	// 4. 更新总览页
	uploadIndexPage(store, baseURL)

	fmt.Fprintf(os.Stderr, "taskobserver: 任务删除完成: %s (归档到 %s)\n", taskSafeName, archivePath)
	return nil
}
