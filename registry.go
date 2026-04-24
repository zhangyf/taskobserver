package taskobserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"objstore"
)

type taskMeta struct {
	Name          string `json:"name"`
	SafeName      string `json:"safe_name"`
	Status        string `json:"status"`
	Progress      int    `json:"progress"`
	StartTime     string `json:"start_time"`
	EndTime       string `json:"end_time,omitempty"`
	PageURL       string `json:"page_url"`
	LastHeartbeat int64  `json:"last_heartbeat,omitempty"`
}

var registryMu sync.Mutex

func loadRegistry(store objstore.Store) []taskMeta {
	data, err := store.GetAll(context.Background(), "tasks/registry.json")
	if err != nil || data == nil {
		return nil
	}
	var tasks []taskMeta
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil
	}
	return tasks
}

func saveRegistry(store objstore.Store, tasks []taskMeta) error {
	data, _ := json.MarshalIndent(tasks, "", "  ")
	return store.PutObject(context.Background(), "tasks/registry.json", data)
}

func upsertRegistry(store objstore.Store, baseURL string, meta taskMeta) {
	registryMu.Lock()
	defer registryMu.Unlock()

	tasks := loadRegistry(store)
	found := false
	for i, t := range tasks {
		if t.SafeName == meta.SafeName {
			tasks[i] = meta
			found = true
			break
		}
	}
	if !found {
		tasks = append(tasks, meta)
	}
	if err := saveRegistry(store, tasks); err != nil {
		fmt.Fprintf(os.Stderr, "taskobserver: save registry: %v\n", err)
	}
}

func uploadIndexPage(store objstore.Store, baseURL string) {
	registryMu.Lock()
	_ = loadRegistry(store)
	content := buildSimpleIndexHTML()
	registryMu.Unlock()

	if err := store.PutObject(context.Background(), "index.html", []byte(content)); err != nil {
		fmt.Fprintf(os.Stderr, "taskobserver: upload index.html: %v\n", err)
	}
}

// buildSimpleIndexHTML 构建总览页（当天任务显示，历史任务默认折叠）
func buildSimpleIndexHTML() string {
	return `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="utf-8">
<title>任务总览</title>
<style>
body{font-family:-apple-system,sans-serif;margin:20px;background:#f5f7fa;font-size:13px}
.box{background:white;border-radius:8px;padding:20px;max-width:1000px;margin:0 auto}
.header{display:flex;justify-content:space-between;margin-bottom:14px;align-items:center}
.title{font-size:15px;font-weight:600}
.meta{font-size:12px;color:#333;font-weight:500;background:#f0f0f0;padding:3px 8px;border-radius:4px}
.ctrl{display:flex;justify-content:flex-end;margin-bottom:14px;font-size:12px;gap:6px;align-items:center}
.ctrl input{width:36px;padding:3px 5px;border:1px solid #ddd;border-radius:3px;text-align:center}
.btn{padding:3px 0;border:1px solid #ddd;border-radius:3px;cursor:pointer;background:#f5f5f5;width:36px;text-align:center;box-sizing:border-box}
.btn.on{background:#1890ff;color:white;border-color:#1890ff}
table{width:100%;border-collapse:collapse;font-size:13px;table-layout:fixed}
col.c-name{width:30%}
col.c-status{width:14%}
col.c-progress{width:14%}
col.c-start{width:21%}
col.c-end{width:21%}
th{text-align:left;padding:7px 10px;background:#fafafa;border-bottom:2px solid #e8e8e8;color:#555;font-weight:600;overflow:hidden}
td{padding:8px 10px;border-bottom:1px solid #f0f0f0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
a{color:#1890ff;text-decoration:none}
a:hover{text-decoration:underline}
.prog-bar{display:inline-block;width:60px;height:5px;background:#e8e8e8;border-radius:3px;vertical-align:middle;margin-right:5px}
.group-today td{background:#e6f7ff;color:#1890ff;font-weight:600;padding:9px 10px}
.group-history td{background:#fff7e6;color:#d48806;font-weight:600;padding:9px 10px;cursor:pointer;user-select:none}
.group-history:hover td{background:#ffefc9}
</style>
</head>
<body>
<div class="box">
  <div class="header">
    <div class="title">📋 任务总览</div>
    <div class="meta" id="meta">正在加载...</div>
  </div>
  <div class="ctrl">
    自动刷新
    <input id="interval" type="number" value="5" min="1" max="60">
    秒
    <button id="toggleBtn" class="btn on" onclick="toggleRefresh()">ON</button>
  </div>
  <table>
    <colgroup>
      <col class="c-name"><col class="c-status"><col class="c-progress"><col class="c-start"><col class="c-end">
    </colgroup>
    <thead>
      <tr><th>任务名</th><th>状态</th><th>进度</th><th>开始时间</th><th>结束时间</th></tr>
    </thead>
    <tbody id="body">
      <tr><td colspan="5" style="text-align:center;padding:30px;color:#bbb">正在加载...</td></tr>
    </tbody>
  </table>
</div>
<script>
var TIMEOUT = 120;
var timer = null;
var autoOn = true;
var historyExpanded = false;
var historyCount = 0;

function statusInfo(s, hb) {
  var now = Math.floor(Date.now() / 1000);
  if (s === 'running' && hb > 0 && now - hb > TIMEOUT)
    return { icon: '💀', text: '已终止(无心跳)', color: '#d48806' };
  if (s === 'completed') return { icon: '✅', text: '已完成', color: '#52c41a' };
  if (s === 'failed') return { icon: '❌', text: '失败', color: '#ff4d4f' };
  if (s === 'killed') return { icon: '💀', text: '已终止', color: '#d48806' };
  return { icon: '🔄', text: '运行中', color: '#1890ff' };
}

function render(tasks) {
  if (!tasks || tasks.length === 0) {
    document.getElementById('body').innerHTML = '<tr><td colspan="5" style="text-align:center;padding:30px;color:#bbb">暂无任务</td></tr>';
    document.getElementById('meta').textContent = '最后更新：' + new Date().toLocaleString() + ' | 共 0 个任务';
    return;
  }
  var today = new Date().toISOString().slice(0, 10);
  var todayRows = [], histRows = [];
  tasks.forEach(function(t) {
    if (t.start_time && t.start_time.startsWith(today)) todayRows.push(t);
    else histRows.push(t);
  });
  historyCount = histRows.length;
  var html = '';
  html += '<tr class="group-today"><td colspan="5">📅 今天（' + todayRows.length + ' 个任务）</td></tr>';
  todayRows.forEach(function(t) {
    var si = statusInfo(t.status, t.last_heartbeat || 0);
    var pct = t.progress || 0;
    html += '<tr>'
      + '<td><a href="' + t.page_url + '" target="_blank">' + t.name + '</a></td>'
      + '<td style="color:' + si.color + '">' + si.icon + ' ' + si.text + '</td>'
      + '<td><div class="prog-bar"><div style="width:' + pct + '%;height:100%;background:' + si.color + ';border-radius:3px"></div></div>' + pct + '%</td>'
      + '<td>' + (t.start_time || '-') + '</td>'
      + '<td>' + (t.end_time || '-') + '</td>'
      + '</tr>';
  });
  if (histRows.length > 0) {
    var icon = historyExpanded ? '▼' : '▶';
    html += '<tr class="group-history" onclick="toggleHistory()">'
          + '<td colspan="5"><span id="hIcon">' + icon + '</span>&nbsp;🗓️ 历史任务（' + histRows.length + ' 个）</td>'
          + '</tr>';
    histRows.forEach(function(t, i) {
      var si = statusInfo(t.status, t.last_heartbeat || 0);
      var pct = t.progress || 0;
      var display = historyExpanded ? '' : 'none';
      html += '<tr id="hr_' + i + '" style="display:' + display + '">'
        + '<td style="background:#fffdf5"><a href="' + t.page_url + '" target="_blank">' + t.name + '</a></td>'
        + '<td style="background:#fffdf5;color:' + si.color + '">' + si.icon + ' ' + si.text + '</td>'
        + '<td style="background:#fffdf5"><div class="prog-bar"><div style="width:' + pct + '%;height:100%;background:' + si.color + ';border-radius:3px"></div></div>' + pct + '%</td>'
        + '<td style="background:#fffdf5">' + (t.start_time || '-') + '</td>'
        + '<td style="background:#fffdf5">' + (t.end_time || '-') + '</td>'
        + '</tr>';
    });
  }
  document.getElementById('body').innerHTML = html;
  document.getElementById('meta').textContent = '最后更新：' + new Date().toLocaleString()
    + ' | 今天: ' + todayRows.length + ' | 历史: ' + histRows.length + ' | 总计: ' + tasks.length + ' 个任务';
}

function toggleHistory() {
  historyExpanded = !historyExpanded;
  var icon = document.getElementById('hIcon');
  if (icon) icon.textContent = historyExpanded ? '▼' : '▶';
  for (var i = 0; i < historyCount; i++) {
    var row = document.getElementById('hr_' + i);
    if (row) row.style.display = historyExpanded ? '' : 'none';
  }
}

function loadTasks() {
  fetch('./tasks/registry.json?t=' + Date.now())
    .then(function(r) { return r.json(); })
    .then(function(tasks) { render(tasks); })
    .catch(function(e) { document.getElementById('meta').textContent = '加载失败: ' + e.message; });
}

function toggleRefresh() {
  if (autoOn) {
    clearInterval(timer); timer = null; autoOn = false;
    document.getElementById('toggleBtn').textContent = 'OFF';
    document.getElementById('toggleBtn').className = 'btn';
  } else {
    startTimer(); autoOn = true;
    document.getElementById('toggleBtn').textContent = 'ON';
    document.getElementById('toggleBtn').className = 'btn on';
  }
}

function startTimer() {
  if (timer) clearInterval(timer);
  var sec = parseInt(document.getElementById('interval').value) || 5;
  timer = setInterval(loadTasks, sec * 1000);
}

document.getElementById('interval').addEventListener('change', function() {
  if (autoOn) startTimer();
});

loadTasks();
startTimer();
</script>
</body>
</html>`
}
