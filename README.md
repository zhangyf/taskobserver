# taskobserver

将任务日志和进度实时推送到腾讯云 COS 静态页面的 Go package。

## 特性

- 实时日志展示（滑动窗口，默认保留最近 1000 行）
- 任务进度条
- 日志分片压缩上传（gzip，默认每 500 行一片）
- 多任务总览页
- 支持自动刷新、日志下载

## 快速集成

```go
import "github.com/zhangyf/taskobserver"

obs := taskobserver.New(taskobserver.Config{
    Bucket:      "my-bucket",
    Region:      "ap-nanjing",
    SecretID:    os.Getenv("TASKOBS_SECRET_ID"),
    SecretKey:   os.Getenv("TASKOBS_SECRET_KEY"),
    BaseURL:     "https://your-domain.com",
    TaskName:    "数据迁移任务",
    ExtraWriter: os.Stderr,
})

log.SetOutput(obs.Writer())
obs.Start(func() (int, int) { return current, total })
defer obs.Done()
```

## 环境变量

| 变量 | 说明 | 必填 |
|------|------|------|
| `TASKOBS_BUCKET` | COS 桶名 | ✅ |
| `TASKOBS_REGION` | COS 地域 | ✅ |
| `TASKOBS_SECRET_ID` | 腾讯云 SecretId | ✅ |
| `TASKOBS_SECRET_KEY` | 腾讯云 SecretKey | ✅ |
| `TASKOBS_BASE_URL` | 自定义域名 | ❌ |
| `TASKOBS_TASK` | 任务名称 | ❌ |
