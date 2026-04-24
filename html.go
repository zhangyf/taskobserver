package taskobserver

import (
	"fmt"
	"html"
	"strings"
	"time"
)

func levelClass(line string) string {
	switch {
	case strings.Contains(line, "level=ERROR"):
		return "level-error"
	case strings.Contains(line, "level=WARN"):
		return "level-warn"
	case strings.Contains(line, "level=DEBUG"):
		return "level-debug"
	default:
		return "level-info"
	}
}

// buildTaskHTML 构建任务详情页 HTML。
// status: running / completed / failed / killed
func buildTaskHTML(taskName string, lines []string, current, total int, status Status, shards []string) string {
	pct := 0
	if total > 0 {
		pct = current * 100 / total
		if pct > 100 {
			pct = 100
		}
	}

	statusText, statusIcon, statusColor, barClass := "Running", "🔄", "#1677ff", ""
	switch status {
	case StatusCompleted:
		statusText, statusIcon, statusColor, barClass = "Completed", "✅", "#52c41a", " done"
		pct = 100
	case StatusFailed:
		statusText, statusIcon, statusColor, barClass = "Failed", "❌", "#cf1322", " failed"
	case StatusKilled:
		statusText, statusIcon, statusColor, barClass = "Killed", "💀", "#874d00", " killed"
	}

	var logLines strings.Builder
	for _, l := range lines {
		logLines.WriteString(fmt.Sprintf("    <div class=\"line %s\">%s</div>\n",
			levelClass(l), html.EscapeString(l)))
	}
	rawEscaped := html.EscapeString(strings.Join(lines, "\n"))
	now := time.Now().Format("2006-01-02 15:04:05 Z07:00")
	downloadName := strings.ReplaceAll(taskName, " ", "_") + ".log"

	var shardsHTML strings.Builder
	if len(shards) > 0 {
		shardsHTML.WriteString("  <div class=\"shards\">\n    <div class=\"shards-title\">📦 日志分片（gzip · COS）</div>\n")
		for i, u := range shards {
			shardsHTML.WriteString(fmt.Sprintf("    <a class=\"shard-link\" href=\"%s\" target=\"_blank\">part-%04d.log.gz</a>\n", u, i+1))
		}
		shardsHTML.WriteString("  </div>\n")
	}

	// 任务结束后停止自动刷新；running 时继续轮询，同时检测心跳超时
	autoRefreshJS := fmt.Sprintf(`startRefresh();
  // 心跳超时检测：若 %d 秒未更新则标记为 Killed
  (function(){
    var HB_TIMEOUT=%d;
    setInterval(function(){
      fetch(location.href+'?t='+Date.now()).then(function(r){return r.text();})
      .then(function(h){
        var doc=new DOMParser().parseFromString(h,'text/html');
        var hbEl=doc.getElementById('heartbeat-ts');
        if(!hbEl)return;
        var ts=parseInt(hbEl.textContent||'0');
        if(ts>0 && (Date.now()/1000-ts)>HB_TIMEOUT){
          var badge=document.getElementById('badge');
          if(badge&&badge.textContent.indexOf('Running')>=0){
            badge.textContent='💀 Killed (no heartbeat)';
            badge.style.color='#874d00';
            stopRefresh();
          }
        }
      }).catch(function(){});
    },15000);
  })();`, HeartbeatTimeout, HeartbeatTimeout)

	if status != StatusRunning {
		autoRefreshJS = "// task finished, auto-refresh stopped"
	}

	heartbeatTS := fmt.Sprintf(`<span id="heartbeat-ts" style="display:none">%d</span>`, time.Now().Unix())

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh"><head><meta charset="utf-8"><title>%s — Task Log</title>
<style>
  *{box-sizing:border-box}
  body{background:#f0f2f5;color:#1a1a1a;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;margin:0;padding:24px}
  .card{background:#fff;border:1px solid #e0e0e0;border-radius:10px;padding:24px;max-width:960px;margin:0 auto;box-shadow:0 2px 8px rgba(0,0,0,.06)}
  .header{display:flex;align-items:center;justify-content:space-between;margin-bottom:16px}
  .title{font-size:18px;font-weight:600;color:#111}
  .back{font-size:12px;color:#1677ff;text-decoration:none;margin-right:12px}
  .back:hover{text-decoration:underline}
  .badge{display:inline-flex;align-items:center;gap:6px;font-size:13px;font-weight:500;padding:4px 12px;border-radius:20px;color:%s;background:%s1a;border:1px solid %s40}
  .meta{font-size:12px;color:#888;margin-bottom:16px}
  .progress-wrap{margin-bottom:20px}
  .progress-label{display:flex;justify-content:space-between;font-size:12px;color:#555;margin-bottom:6px}
  .progress-track{height:8px;background:#e8e8e8;border-radius:4px;overflow:hidden}
  .progress-bar{height:100%%;border-radius:4px;background:linear-gradient(90deg,#1677ff,#4096ff);transition:width .4s ease}
  .progress-bar.done{background:linear-gradient(90deg,#52c41a,#73d13d)}
  .progress-bar.failed{background:linear-gradient(90deg,#cf1322,#ff4d4f)}
  .progress-bar.killed{background:linear-gradient(90deg,#874d00,#d46b08)}
  .toolbar{display:flex;align-items:center;justify-content:space-between;margin-bottom:8px}
  .toolbar-left{font-size:12px;color:#888}
  .toolbar-right{display:flex;align-items:center;gap:10px}
  .refresh-ctrl{display:inline-flex;align-items:center;gap:4px;font-size:12px;color:#555}
  .refresh-ctrl .label{white-space:nowrap}
  .refresh-ctrl input{width:36px;padding:2px 4px;font-size:12px;border:1px solid #d0d0d0;border-radius:4px;text-align:center;flex-shrink:0}
  .btn-toggle{font-size:12px;padding:0;width:36px;height:22px;line-height:22px;text-align:center;border-radius:4px;border:1px solid #d0d0d0;cursor:pointer;background:#fafafa;color:#444;box-sizing:border-box;flex-shrink:0;display:inline-block}
  .btn-toggle.active{background:#1677ff;color:#fff;border-color:#1677ff}
  .btn-download{display:inline-flex;align-items:center;gap:5px;font-size:12px;padding:5px 12px;border:1px solid #d0d0d0;border-radius:6px;background:#fafafa;color:#444;cursor:pointer;text-decoration:none;transition:background .15s}
  .btn-download:hover{background:#f0f0f0}
  .log{background:#fafafa;border:1px solid #e8e8e8;border-radius:6px;padding:14px 16px;font-family:"JetBrains Mono","Fira Code",Consolas,monospace;font-size:12.5px;line-height:1.2;height:420px;overflow-y:auto;white-space:pre-wrap;word-break:break-all}
  .line{padding:0;margin:0}
  .level-info{color:#1a1a1a}
  .level-warn{color:#d46b08}
  .level-error{color:#cf1322;font-weight:500}
  .level-debug{color:#aaa}
  .shards{margin-top:16px;padding:12px 16px;background:#f6f8fa;border:1px solid #e0e0e0;border-radius:6px}
  .shards-title{font-size:12px;font-weight:600;color:#555;margin-bottom:8px}
  .shard-link{display:inline-block;font-size:12px;margin:2px 6px 2px 0;padding:2px 8px;background:#fff;border:1px solid #d0d0d0;border-radius:4px;color:#1677ff;text-decoration:none}
  .shard-link:hover{background:#e6f4ff}
</style></head><body>
<div class="card">
  <div class="header">
    <span><a class="back" href="../../index.html">← 返回总览</a><span class="title">📋 %s</span></span>
    <span class="badge" id="badge">%s %s</span>
  </div>
  <div class="meta" id="meta">最后更新：%s %s</div>
  <div class="progress-wrap">
    <div class="progress-label" id="progress-label"><span>进度</span><span>%d / %d &nbsp;(%d%%)</span></div>
    <div class="progress-track"><div class="progress-bar%s" id="progress-bar" style="width:%d%%"></div></div>
  </div>
  <div class="toolbar">
    <span class="toolbar-left" id="log-count">最近 %d 行（窗口 %d）</span>
    <div class="toolbar-right">
      <span class="refresh-ctrl"><span class="label">自动刷新</span><input id="ri" type="number" value="3" min="1" max="60"><span class="label">秒</span><button id="rb" class="btn-toggle active">ON</button></span>
      <a class="btn-download" id="dl" href="#" download="%s">⬇ 下载窗口日志</a>
    </div>
  </div>
  <pre id="rawlog" style="display:none">%s</pre>
  <div class="log" id="log">
%s  </div>
%s</div>
<script>
  var log=document.getElementById('log'),atBottom=true;
  log.addEventListener('scroll',function(){atBottom=log.scrollTop+log.clientHeight>=log.scrollHeight-10;});
  function scrollBottom(){if(atBottom)log.scrollTop=log.scrollHeight;}
  scrollBottom();
  function rebuildDownload(){
    var dl=document.getElementById('dl');if(!dl)return;
    var blob=new Blob([document.getElementById('rawlog').textContent],{type:'text/plain'});
    dl.href=URL.createObjectURL(blob);
  }
  rebuildDownload();
  var timer=null,autoOn=true;
  var rb=document.getElementById('rb'),ri=document.getElementById('ri');
  function getInterval(){return Math.max(1,parseInt(ri.value)||3)*1000;}
  function doRefresh(){
    fetch(location.href+'?t='+Date.now()).then(function(r){return r.text();}).then(function(h){
      var doc=new DOMParser().parseFromString(h,'text/html');
      ['log','rawlog','meta','badge','progress-label','log-count'].forEach(function(id){
        var n=doc.getElementById(id),o=document.getElementById(id);if(n&&o)o.innerHTML=n.innerHTML;
      });
      var nb=doc.getElementById('progress-bar'),ob=document.getElementById('progress-bar');
      if(nb&&ob){ob.style.width=nb.style.width;ob.className=nb.className;}
      var ns=doc.querySelector('.shards'),os=document.querySelector('.shards');
      if(ns&&os)os.outerHTML=ns.outerHTML;
      else if(ns)document.querySelector('.card').appendChild(ns.cloneNode(true));
      scrollBottom();rebuildDownload();
      var badge=document.getElementById('badge');
      if(badge&&(badge.textContent.indexOf('Completed')>=0||badge.textContent.indexOf('Failed')>=0||badge.textContent.indexOf('Killed')>=0))stopRefresh();
    }).catch(function(){});
  }
  function startRefresh(){if(timer)clearInterval(timer);timer=setInterval(doRefresh,getInterval());autoOn=true;rb.textContent='ON';rb.style.background='#1890ff';rb.style.color='white';rb.style.borderColor='#1890ff';}
  function stopRefresh(){if(timer){clearInterval(timer);timer=null;}autoOn=false;rb.textContent='OFF';rb.style.background='#fafafa';rb.style.color='#444';rb.style.borderColor='#d0d0d0';}
  rb.addEventListener('click',function(){autoOn?stopRefresh():startRefresh();});
  ri.addEventListener('change',function(){if(autoOn)startRefresh();});
  %s
</script>
</body></html>
`,
		html.EscapeString(taskName),
		statusColor, statusColor, statusColor,
		html.EscapeString(taskName),
		statusIcon, statusText,
		now, heartbeatTS,
		current, total, pct,
		barClass, pct,
		len(lines), windowSize,
		downloadName,
		rawEscaped,
		logLines.String(),
		shardsHTML.String(),
		autoRefreshJS,
	)
}
