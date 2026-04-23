package taskobserver

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type taskMeta struct {
	Name      string `json:"name"`
	SafeName  string `json:"safe_name"`
	Status    string `json:"status"`
	Progress  int    `json:"progress"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time,omitempty"`
	PageURL   string `json:"page_url"`
}

var registryMu sync.Mutex

func loadRegistry(c *cosClient) []taskMeta {
	data, err := c.getJSON("tasks/registry.json")
	if err != nil || data == nil {
		return nil
	}
	var tasks []taskMeta
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil
	}
	return tasks
}

func saveRegistry(c *cosClient, tasks []taskMeta) error {
	data, _ := json.MarshalIndent(tasks, "", "  ")
	return c.putString("tasks/registry.json", "application/json; charset=utf-8", string(data))
}

func upsertRegistry(c *cosClient, baseURL string, meta taskMeta) {
	registryMu.Lock()
	defer registryMu.Unlock()

	tasks := loadRegistry(c)
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
	if err := saveRegistry(c, tasks); err != nil {
		fmt.Fprintf(os.Stderr, "taskobserver: save registry: %v\n", err)
	}
}

func uploadIndexPage(c *cosClient, baseURL string) {
	registryMu.Lock()
	tasks := loadRegistry(c)
	registryMu.Unlock()
	content := buildIndexHTML(tasks, baseURL)
	if err := c.putString("index.html", "text/html; charset=utf-8", content); err != nil {
		fmt.Fprintf(os.Stderr, "taskobserver: upload index.html: %v\n", err)
	}
}

func buildIndexHTML(tasks []taskMeta, baseURL string) string {
	_ = baseURL
	var rows string
	for _, t := range tasks {
		icon, color, statusText := "🔄", "#1677ff", "Running"
		if t.Status == "completed" {
			icon, color, statusText = "✅", "#52c41a", "Completed"
		} else if t.Status == "failed" {
			icon, color, statusText = "❌", "#cf1322", "Failed"
		}
		endCell := "-"
		if t.EndTime != "" {
			endCell = t.EndTime
		}
		rows += fmt.Sprintf(`    <tr>
      <td><a href="%s" target="_blank">%s</a></td>
      <td><span style="color:%s;font-weight:500">%s %s</span></td>
      <td>
        <div class="mini-track"><div class="mini-bar" style="width:%d%%;background:%s"></div></div>
        <span class="pct">%d%%</span>
      </td>
      <td>%s</td>
      <td>%s</td>
    </tr>
`, t.PageURL, t.Name, color, icon, statusText, t.Progress, color, t.Progress, t.StartTime, endCell)
	}
	if len(tasks) == 0 {
		rows = `    <tr><td colspan="5" style="text-align:center;color:#aaa;padding:32px">暂无任务</td></tr>`
	}
	now := time.Now().Format("2006-01-02 15:04:05 Z07:00")
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh"><head><meta charset="utf-8"><title>任务总览</title>
<style>
  *{box-sizing:border-box}
  body{background:#f0f2f5;color:#1a1a1a;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;margin:0;padding:24px}
  .card{background:#fff;border:1px solid #e0e0e0;border-radius:10px;padding:24px;max-width:960px;margin:0 auto;box-shadow:0 2px 8px rgba(0,0,0,.06)}
  .header{display:flex;align-items:center;justify-content:space-between;margin-bottom:20px}
  .title{font-size:18px;font-weight:600;color:#111}
  .meta{font-size:12px;color:#888}
  table{width:100%%;border-collapse:collapse}
  th{text-align:left;font-size:12px;font-weight:600;color:#555;padding:8px 12px;border-bottom:2px solid #e8e8e8;background:#fafafa}
  td{font-size:13px;padding:10px 12px;border-bottom:1px solid #f0f0f0;vertical-align:middle}
  tr:last-child td{border-bottom:none}
  tr:hover td{background:#fafafa}
  td a{color:#1677ff;text-decoration:none;font-weight:500}
  td a:hover{text-decoration:underline}
  .mini-track{display:inline-block;width:80px;height:6px;background:#e8e8e8;border-radius:3px;overflow:hidden;vertical-align:middle;margin-right:6px}
  .mini-bar{height:100%%;border-radius:3px}
  .pct{font-size:12px;color:#888}
  .toolbar{display:flex;align-items:center;justify-content:flex-end;gap:8px;margin-bottom:12px}
  .refresh-ctrl{display:flex;align-items:center;gap:4px;font-size:12px;color:#555}
  .refresh-ctrl input{width:40px;padding:2px 4px;font-size:12px;border:1px solid #d0d0d0;border-radius:4px;text-align:center}
  .btn-toggle{font-size:12px;padding:3px 10px;border-radius:4px;border:1px solid #d0d0d0;cursor:pointer;background:#fafafa;color:#444;transition:all .15s}
  .btn-toggle.active{background:#1677ff;color:#fff;border-color:#1677ff}
</style></head><body>
<div class="card">
  <div class="header">
    <span class="title">📋 任务总览</span>
    <span class="meta" id="meta">最后更新：%s &nbsp;|&nbsp; 共 %d 个任务</span>
  </div>
  <div class="toolbar">
    <span class="refresh-ctrl">自动刷新 <input id="ri" type="number" value="5" min="1" max="60">秒
    <button id="rb" class="btn-toggle active">ON</button></span>
  </div>
  <table>
    <thead><tr><th>任务名</th><th>状态</th><th>进度</th><th>开始时间</th><th>结束时间</th></tr></thead>
    <tbody id="tbody">
%s    </tbody>
  </table>
</div>
<script>
  var timer=null,autoOn=true;
  var rb=document.getElementById('rb'),ri=document.getElementById('ri');
  function getInterval(){return Math.max(1,parseInt(ri.value)||5)*1000;}
  function doRefresh(){
    fetch(location.href+'?t='+Date.now()).then(function(r){return r.text();}).then(function(h){
      var doc=new DOMParser().parseFromString(h,'text/html');
      var nb=doc.getElementById('tbody'),ob=document.getElementById('tbody');
      if(nb&&ob)ob.innerHTML=nb.innerHTML;
      var nm=doc.getElementById('meta'),om=document.getElementById('meta');
      if(nm&&om)om.textContent=nm.textContent;
    }).catch(function(){});
  }
  function startRefresh(){if(timer)clearInterval(timer);timer=setInterval(doRefresh,getInterval());autoOn=true;rb.textContent='ON';rb.className='btn-toggle active';}
  function stopRefresh(){if(timer){clearInterval(timer);timer=null;}autoOn=false;rb.textContent='OFF';rb.className='btn-toggle';}
  rb.addEventListener('click',function(){autoOn?stopRefresh():startRefresh();});
  ri.addEventListener('change',function(){if(autoOn)startRefresh();});
  startRefresh();
</script>
</body></html>
`, now, len(tasks), rows)
}
