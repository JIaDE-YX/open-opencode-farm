package main

// Web 管理面板：登录 + 状态总览 + 配置编辑/热重载
// 路由：/admin, /admin/login, /admin/logout, /api/admin/status, /api/admin/config

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const adminCookieName = "oc2a_admin"

// 会话：内存 token -> 过期时间
type adminSession struct {
	expires time.Time
}

func (g *Gateway) adminAuth(r *http.Request) bool {
	if g.cfg.AdminPassword == "" {
		return false
	}
	c, err := r.Cookie(adminCookieName)
	if err != nil {
		return false
	}
	g.adminMu.RLock()
	defer g.adminMu.RUnlock()
	s, ok := g.adminSessions[c.Value]
	if !ok || time.Now().After(s.expires) {
		return false
	}
	return true
}

func (g *Gateway) createAdminSession(w http.ResponseWriter) {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	token := hex.EncodeToString(buf)
	g.adminMu.Lock()
	if g.adminSessions == nil {
		g.adminSessions = map[string]adminSession{}
	}
	g.adminSessions[token] = adminSession{expires: time.Now().Add(24 * time.Hour)}
	// 清理过期
	for k, s := range g.adminSessions {
		if time.Now().After(s.expires) {
			delete(g.adminSessions, k)
		}
	}
	g.adminMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: adminCookieName, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		MaxAge: 86400,
	})
}

func (g *Gateway) destroyAdminSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(adminCookieName); err == nil {
		g.adminMu.Lock()
		delete(g.adminSessions, c.Value)
		g.adminMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: adminCookieName, Value: "", Path: "/", MaxAge: -1})
}

// ---------- 路由处理 ----------

func (g *Gateway) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	if !g.adminAuth(r) {
		g.renderLoginPage(w, "")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(adminPageHTML))
}

func (g *Gateway) renderLoginPage(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(fmt.Sprintf(adminLoginHTML, msg)))
}

func (g *Gateway) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	password := r.FormValue("password")
	if g.cfg.AdminPassword == "" {
		http.Error(w, "admin panel disabled (admin_password not set)", http.StatusForbidden)
		return
	}
	if subtle.ConstantTimeCompare([]byte(password), []byte(g.cfg.AdminPassword)) == 1 {
		g.createAdminSession(w)
		http.Redirect(w, r, "/admin", http.StatusFound)
		return
	}
	g.renderLoginPage(w, "密码错误")
}

func (g *Gateway) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	g.destroyAdminSession(w, r)
	http.Redirect(w, r, "/admin", http.StatusFound)
}

type adminStatusResponse struct {
	Ready      bool              `json:"ready"`
	Version    string            `json:"version"`
	Keys       healthKeys        `json:"keys"`
	Proxies    healthProxies     `json:"proxies"`
	Models     healthModels      `json:"models"`
	ConfigPath string            `json:"config_path"`
	Prefer     string            `json:"prefer"`
	Upstream   map[string]string `json:"upstream"`
	UptimeSec  int64             `json:"uptime_sec"`
}

func (g *Gateway) handleAdminStatus(w http.ResponseWriter, _ *http.Request) {
	models := g.catalog.Snapshot()
	proxyTotal, proxyHealthy := g.transports.healthCounts()
	zenKeys, goKeys := g.zenNodes.Len(), g.goNodes.Len()
	staleAfter := max(2*time.Duration(g.cfg.Models.RefreshSeconds)*time.Second, time.Minute)

	modelStatus := "ready"
	var lastRefresh *time.Time
	if models.UpdatedAt.IsZero() {
		modelStatus = "pending"
	} else {
		updatedAt := models.UpdatedAt.UTC()
		lastRefresh = &updatedAt
		if models.Exposed == 0 {
			modelStatus = "empty"
		} else if time.Since(models.UpdatedAt) > staleAfter {
			modelStatus = "stale"
		}
	}
	ready := models.Exposed > 0 && (zenKeys+goKeys) > 0 && proxyHealthy > 0

	resp := adminStatusResponse{
		Ready:   ready,
		Version: version,
		Keys:    healthKeys{Zen: zenKeys, Go: goKeys, Total: zenKeys + goKeys},
		Proxies: healthProxies{Total: proxyTotal, Healthy: proxyHealthy, Unhealthy: proxyTotal - proxyHealthy},
		Models: healthModels{
			Status: modelStatus, Total: models.Total, Exposed: models.Exposed,
			Zen: models.Zen, Go: models.Go, LastRefresh: lastRefresh,
			StaleAfterSeconds: int(staleAfter.Seconds()),
		},
		ConfigPath: g.configPath,
		Prefer:     string(g.cfg.Prefer),
		Upstream:   map[string]string{"zen": g.cfg.Upstream.Zen, "go": g.cfg.Upstream.Go},
		UptimeSec:  int64(time.Since(g.startedAt).Seconds()),
	}
	writeJSON(w, http.StatusOK, resp)
}

func maskSecret(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "..." + s[len(s)-4:]
}

func maskProxyURL(raw string) string {
	// http://user:pass@host:port -> http://user:****@host:port
	if i := strings.Index(raw, "@"); i != -1 {
		head := raw[:i]
		if j := strings.LastIndex(head, ":"); j != -1 {
			return head[:j+1] + "****" + raw[i:]
		}
	}
	return raw
}

type adminConfigResponse struct {
	Listen      string   `json:"listen"`
	ServerKeys  []string `json:"server_keys"`
	ZenKeys     []string `json:"zen_keys"`
	GoKeys      []string `json:"go_keys"`
	Proxies     []string `json:"proxies"`
	Prefer      string   `json:"prefer"`
	RawJSON     string   `json:"raw_json"` // 完整可编辑 JSON（敏感字段用占位符）
}

func (g *Gateway) handleAdminConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := g.cfg
		resp := adminConfigResponse{
			Listen:     cfg.Listen,
			ServerKeys: maskList(cfg.ServerKeys),
			ZenKeys:    maskList(cfg.ZenKeys),
			GoKeys:     maskList(cfg.GoKeys),
			Proxies:    maskList(cfg.Proxies),
			Prefer:     string(cfg.Prefer),
		}
		raw, err := os.ReadFile(g.configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// 编辑用原始 JSON；敏感字段替换为占位符提示
		editable := string(raw)
		resp.RawJSON = editable
		writeJSON(w, http.StatusOK, resp)

	case http.MethodPost:
		// body: {"json": "<完整 config.json 内容>"}
		var body struct {
			JSON string `json:"json"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
			return
		}
		var newCfg Config
		dec := json.NewDecoder(strings.NewReader(body.JSON))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&newCfg); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 解析失败: " + err.Error()})
			return
		}
		if err := newCfg.Validate(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// 备份 + 写入
		if _, err := os.Stat(g.configPath); err == nil {
			_ = os.WriteFile(g.configPath+".bak-web", mustRead(g.configPath), 0o644)
		}
		if err := os.WriteFile(g.configPath, []byte(body.JSON), 0o644); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if err := g.Reload(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "配置已保存但重载失败: " + err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved_and_reloaded"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (g *Gateway) handleAdminReload(w http.ResponseWriter, _ *http.Request) {
	if err := g.Reload(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
}

func (g *Gateway) handleAdminModels(w http.ResponseWriter, _ *http.Request) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	zen, goModels := g.catalog.ListByTier()
	writeJSON(w, http.StatusOK, map[string]any{"zen": zen, "go": goModels})
}

// ---------- 代理与 key 绑定明细 ----------

type adminProxyInfo struct {
	Index   int    `json:"index"`
	Name    string `json:"name"` // masked
	Healthy bool   `json:"healthy"`
}

type adminNodeInfo struct {
	Index        int    `json:"index"`
	Tier         string `json:"tier"` // zen / go
	Key          string `json:"key"`  // masked
	ProxyIndex   int    `json:"proxy_index"`
	Failures     uint32 `json:"failures"`
	CooldownSecs int64  `json:"cooldown_secs"`
}

func (g *Gateway) handleAdminProxies(w http.ResponseWriter, _ *http.Request) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	items := make([]adminProxyInfo, 0, len(g.transports.items))
	for _, p := range g.transports.items {
		items = append(items, adminProxyInfo{
			Index:   p.index,
			Name:    maskProxyURL(p.name),
			Healthy: p.healthy.Load(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (g *Gateway) handleAdminNodes(w http.ResponseWriter, _ *http.Request) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	now := time.Now().UnixMilli()
	var items []adminNodeInfo
	collect := func(tier string, pool *nodePool) {
		for _, n := range pool.nodes {
			items = append(items, adminNodeInfo{
				Index:        n.index,
				Tier:         tier,
				Key:          maskSecret(n.key),
				ProxyIndex:   int(n.proxyIndex.Load()),
				Failures:     n.failures.Load(),
				CooldownSecs: max(0, (n.cooldownUntil.Load()-now+999)/1000),
			})
		}
	}
	collect("zen", g.zenNodes)
	collect("go", g.goNodes)
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// Reload 从磁盘重新加载 config.json 并热重建代理池/节点池/目录
func (g *Gateway) Reload() error {
	cfg, err := LoadConfig(g.configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := g.logger

	transports, err := newTransportPool(cfg.Proxies, cfg.Performance, time.Duration(cfg.Retry.TimeoutSeconds)*time.Second)
	if err != nil {
		return fmt.Errorf("transport pool: %w", err)
	}
	cooldown := time.Duration(cfg.Performance.FailureCooldownSeconds) * time.Second
	zenNodes, err := newNodePool(cfg.ZenKeys, transports, cooldown)
	if err != nil {
		return fmt.Errorf("zen node pool: %w", err)
	}
	goNodes, err := newNodePool(cfg.GoKeys, transports, cooldown)
	if err != nil {
		return fmt.Errorf("go node pool: %w", err)
	}
	catalog := newModelCatalog(cfg.Prefer, cfg.Models.Protocols)

	g.mu.Lock()
	g.cfg = cfg
	g.transports = transports
	g.zenNodes = zenNodes
	g.goNodes = goNodes
	g.catalog = catalog
	g.mu.Unlock()
	logger.Info("admin: config reloaded",
		"zen_keys", len(cfg.ZenKeys), "go_keys", len(cfg.GoKeys), "proxies", len(cfg.Proxies))

	// 立即异步刷新模型目录（避免等下一个 refresh 周期）
	upZen, upGo := cfg.Upstream.Zen, cfg.Upstream.Go
	zNodes, gNodes := zenNodes, goNodes
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		var zen, goModels []string
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); zen = g.refreshTier(ctx, upZen, zNodes) }()
		go func() { defer wg.Done(); goModels = g.refreshTier(ctx, upGo, gNodes) }()
		wg.Wait()
		if zen != nil || goModels != nil {
			g.catalog.Replace(zen, goModels)
			logger.Info("admin: catalog refreshed after reload", "models", len(g.catalog.List()))
		}
	}()
	return nil
}

func maskList(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = maskSecret(s)
	}
	return out
}

func mustRead(p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		return []byte{}
	}
	return b
}

// ---------- 前端 ----------

const adminLoginHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>opencode-farm · 登录</title>
<style>
:root{--bg:#09090b;--surface:#131418;--surface-2:#1a1b21;--border:#26272e;--text:#f2f3f5;--text-2:#a1a3ad;--accent:#6e7cf3;--accent-hover:#5a68e8}
*{box-sizing:border-box;margin:0}
body{font-family:-apple-system,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif;background:
radial-gradient(1200px 600px at 70% -10%,rgba(110,124,243,.08),transparent 60%),var(--bg);color:var(--text);
display:flex;align-items:center;justify-content:center;min-height:100vh}
.card{width:340px;padding:36px 32px;border-radius:14px;background:linear-gradient(180deg,var(--surface),#101116);
border:1px solid var(--border);box-shadow:0 24px 60px rgba(0,0,0,.5)}
.logo{display:flex;align-items:center;gap:10px;margin-bottom:6px}
.logo-mark{width:34px;height:34px;border-radius:9px;background:linear-gradient(135deg,#6e7cf3,#8b5cf6);
display:flex;align-items:center;justify-content:center;font-size:17px;box-shadow:0 4px 14px rgba(110,124,243,.35)}
h1{font-size:17px;font-weight:650;letter-spacing:.2px}
.sub{color:var(--text-2);font-size:13px;margin:10px 0 22px}
input{width:100%;padding:11px 13px;border-radius:9px;border:1px solid var(--border);background:var(--surface-2);
color:var(--text);font-size:14px;outline:none;transition:border-color .15s,box-shadow .15s;margin-bottom:14px}
input:focus{border-color:var(--accent);box-shadow:0 0 0 3px rgba(110,124,243,.15)}
button{width:100%;padding:11px;border:0;border-radius:9px;background:var(--accent);color:#fff;font-size:14px;
font-weight:600;cursor:pointer;transition:background .15s}
button:hover{background:var(--accent-hover)}
.err{color:#f87171;font-size:13px;margin-bottom:12px;background:rgba(248,113,113,.08);border:1px solid rgba(248,113,113,.2);
padding:9px 12px;border-radius:8px}
</style></head><body>
<div class="card">
<div class="logo"><div class="logo-mark">⚡</div><h1>opencode-farm</h1></div>
<div class="sub">管理面板 · 输入管理密码</div>
<form method="post" action="/admin/login">
%s
<input type="password" name="password" placeholder="管理密码" autofocus required>
<button type="submit">登录</button>
</form></div></body></html>`

// 面板 HTML：Linear/Vercel 风格暗色设计，Tab 布局，无外部依赖
const adminPageHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>opencode-farm 管理面板</title>
<style>
:root{
--bg:#09090b;--surface:#121317;--surface-2:#191a20;--surface-3:#202128;
--border:#26272e;--border-2:#32333c;--text:#f2f3f5;--text-2:#9d9faa;--text-3:#666873;
--accent:#6e7cf3;--accent-soft:rgba(110,124,243,.12);--accent-border:rgba(110,124,243,.35);
--ok:#34d399;--ok-soft:rgba(52,211,153,.1);--warn:#fbbf24;--warn-soft:rgba(251,191,36,.1);
--bad:#f87171;--bad-soft:rgba(248,113,113,.1);
--mono:ui-monospace,'Cascadia Code',Consolas,monospace;
--radius:12px;--radius-sm:8px
}
*{box-sizing:border-box;margin:0}
body{font-family:-apple-system,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif;background:
radial-gradient(1400px 700px at 80% -10%,rgba(110,124,243,.07),transparent 55%),var(--bg);
color:var(--text);min-height:100vh;-webkit-font-smoothing:antialiased}
.wrap{max-width:1120px;margin:0 auto;padding:0 24px 60px}

/* ── 顶栏 ── */
.topbar{display:flex;align-items:center;justify-content:space-between;padding:18px 0 16px;border-bottom:1px solid var(--border);margin-bottom:20px}
.brand{display:flex;align-items:center;gap:11px}
.logo-mark{width:36px;height:36px;border-radius:10px;background:linear-gradient(135deg,#6e7cf3,#8b5cf6);
display:flex;align-items:center;justify-content:center;font-size:18px;box-shadow:0 4px 16px rgba(110,124,243,.35)}
.brand h1{font-size:16px;font-weight:650;letter-spacing:.2px}
.brand .ver{color:var(--text-3);font-size:12px;margin-top:2px;font-family:var(--mono)}
.top-actions{display:flex;align-items:center;gap:12px}
.badge{display:inline-flex;align-items:center;gap:6px;padding:4px 11px;border-radius:99px;font-size:12px;font-weight:600}
.badge .dot{width:6px;height:6px;border-radius:50%}
.badge.ok{background:var(--ok-soft);color:var(--ok)}.badge.ok .dot{background:var(--ok);box-shadow:0 0 8px var(--ok)}
.badge.bad{background:var(--bad-soft);color:var(--bad)}.badge.bad .dot{background:var(--bad)}
.badge.warn{background:var(--warn-soft);color:var(--warn)}.badge.warn .dot{background:var(--warn)}
.btn{padding:7px 14px;border-radius:var(--radius-sm);border:1px solid var(--border);background:var(--surface-2);
color:var(--text);font-size:13px;cursor:pointer;transition:border-color .15s,background .15s;font-family:inherit}
.btn:hover{border-color:var(--border-2);background:var(--surface-3)}
.btn.primary{background:var(--accent);border-color:transparent;color:#fff;font-weight:600}
.btn.primary:hover{background:#5a68e8}
.btn:disabled{opacity:.5;cursor:not-allowed}

/* ── Tab ── */
.tabs{display:flex;gap:4px;margin-bottom:20px;background:var(--surface);border:1px solid var(--border);
border-radius:10px;padding:4px;width:fit-content}
.tab{padding:7px 16px;border-radius:7px;border:0;background:transparent;color:var(--text-2);font-size:13px;
cursor:pointer;transition:all .15s;font-family:inherit}
.tab:hover{color:var(--text)}
.tab.active{background:var(--surface-3);color:var(--text);font-weight:600;box-shadow:0 1px 3px rgba(0,0,0,.3)}
.pane{display:none}.pane.active{display:block}

/* ── 指标卡 ── */
.metrics{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:12px;margin-bottom:22px}
.metric{background:linear-gradient(180deg,var(--surface),#101116);border:1px solid var(--border);border-radius:var(--radius);
padding:16px 18px;transition:border-color .2s}
.metric:hover{border-color:var(--border-2)}
.metric .label{color:var(--text-3);font-size:12px;margin-bottom:8px;display:flex;align-items:center;gap:6px}
.metric .label .icon{opacity:.8}
.metric .value{font-size:24px;font-weight:700;letter-spacing:.2px;font-variant-numeric:tabular-nums}
.metric .value small{font-size:13px;font-weight:500;color:var(--text-3);margin-left:3px}
.metric .value.ok{color:var(--ok)}.metric .value.warn{color:var(--warn)}.metric .value.bad{color:var(--bad)}
.metric .sub{color:var(--text-3);font-size:11.5px;margin-top:5px;font-family:var(--mono)}

/* ── 区块卡片 ── */
.section{background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);margin-bottom:18px;overflow:hidden}
.section-head{padding:13px 18px;border-bottom:1px solid var(--border);display:flex;align-items:center;justify-content:space-between}
.section-head .t{font-size:13.5px;font-weight:600}
.section-head .hint{color:var(--text-3);font-size:12px}
.section-body{padding:8px 0}

/* ── 表格 ── */
table{width:100%;border-collapse:collapse;font-size:13px}
th{color:var(--text-3);font-weight:500;font-size:11.5px;text-align:left;padding:9px 18px;border-bottom:1px solid var(--border);text-transform:uppercase;letter-spacing:.6px}
td{padding:10px 18px;border-bottom:1px solid rgba(38,39,46,.6);vertical-align:middle}
tr:last-child td{border-bottom:0}
tr:hover td{background:rgba(255,255,255,.015)}
.mono{font-family:var(--mono);font-size:12.5px}
.muted{color:var(--text-3)}
.dot-lg{width:8px;height:8px;border-radius:50%;display:inline-block;margin-right:8px;vertical-align:middle}
.dot-lg.ok{background:var(--ok);box-shadow:0 0 6px rgba(52,211,153,.6)}
.dot-lg.bad{background:var(--bad);box-shadow:0 0 6px rgba(248,113,113,.6)}
.tag{display:inline-flex;padding:2px 9px;border-radius:99px;font-size:11.5px;font-weight:600;letter-spacing:.3px}
.tag.zen{background:rgba(139,92,246,.14);color:#c4b5fd;border:1px solid rgba(139,92,246,.25)}
.tag.go{background:rgba(34,211,238,.1);color:#67e8f9;border:1px solid rgba(34,211,238,.22)}
.tag.proxy{background:rgba(255,255,255,.05);color:var(--text-2);border:1px solid var(--border)}
.fail-0{color:var(--text-3)}.fail-1{color:var(--warn)}.fail-2{color:var(--bad)}

/* ── 模型目录 ── */
.search{margin:12px 18px;padding:9px 13px;border-radius:var(--radius-sm);border:1px solid var(--border);
background:var(--surface-2);color:var(--text);font-size:13px;width:calc(100% - 36px);outline:none;font-family:inherit}
.search:focus{border-color:var(--accent);box-shadow:0 0 0 3px rgba(110,124,243,.12)}
.model-groups{padding:4px 18px 14px}
.model-group{margin-bottom:14px}
.model-group .ghead{display:flex;align-items:center;gap:8px;margin:10px 0 8px;font-size:12.5px;color:var(--text-2)}
.model-group .ghead .count{color:var(--text-3);font-size:11.5px;font-family:var(--mono)}
.chips{display:flex;flex-wrap:wrap;gap:6px}
.chip{padding:4px 10px;border-radius:7px;background:var(--surface-2);border:1px solid var(--border);
font-family:var(--mono);font-size:12px;color:#c9cbd4;transition:all .12s;cursor:default}
.chip:hover{border-color:var(--accent-border);color:var(--text)}
.chip.hidden{display:none}
.empty{padding:22px;text-align:center;color:var(--text-3);font-size:13px}

/* ── 配置编辑器 ── */
.editor-wrap{padding:14px 18px 18px}
textarea{width:100%;height:400px;background:#0d0e12;color:#d6d8e0;border:1px solid var(--border);border-radius:10px;
padding:14px 16px;font-family:var(--mono);font-size:12.5px;line-height:1.65;resize:vertical;outline:none;
white-space:pre;tab-size:2}
textarea:focus{border-color:var(--accent);box-shadow:0 0 0 3px rgba(110,124,243,.08)}
.editor-actions{display:flex;align-items:center;gap:10px;margin-top:12px}
.hint{color:var(--text-3);font-size:12px}
.path-line{font-family:var(--mono);font-size:11.5px;color:var(--text-3)}

/* ── Toast ── */
#toasts{position:fixed;right:20px;bottom:20px;display:flex;flex-direction:column;gap:8px;z-index:99}
.toast{padding:11px 16px;border-radius:10px;font-size:13px;background:var(--surface-2);border:1px solid var(--border-2);
box-shadow:0 10px 30px rgba(0,0,0,.45);display:flex;align-items:center;gap:9px;animation:slidein .2s ease;max-width:380px}
.toast.ok{border-color:rgba(52,211,153,.35)}.toast.bad{border-color:rgba(248,113,113,.4)}
@keyframes slidein{from{transform:translateY(8px);opacity:0}to{transform:none;opacity:1}}

::-webkit-scrollbar{width:10px;height:10px}
::-webkit-scrollbar-thumb{background:#2c2d35;border-radius:5px;border:2px solid var(--surface)}
::-webkit-scrollbar-track{background:transparent}
</style></head><body>
<div class="wrap">

<!-- 顶栏 -->
<div class="topbar">
<div class="brand">
<div class="logo-mark">⚡</div>
<div><h1>opencode-farm</h1><div class="ver">v<span id="ver">-</span> · <span id="path">-</span></div></div>
</div>
<div class="top-actions">
<span id="readyBadge" class="badge"><span class="dot"></span><span id="readyText">…</span></span>
<a href="/admin/logout" style="text-decoration:none"><button class="btn">退出</button></a>
</div>
</div>

<!-- Tab -->
<div class="tabs">
<button class="tab active" data-tab="overview">总览</button>
<button class="tab" data-tab="proxies">代理与 Key</button>
<button class="tab" data-tab="models">模型目录</button>
<button class="tab" data-tab="config">配置</button>
</div>

<!-- 总览 -->
<div class="pane active" id="pane-overview">
<div class="metrics">
<div class="metric"><div class="label"><span class="icon">●</span>运行状态</div><div class="value" id="m-ready">-</div><div class="sub" id="m-ready-sub">-</div></div>
<div class="metric"><div class="label"><span class="icon">🔑</span>Key 池</div><div class="value" id="m-keys">-</div><div class="sub" id="m-keys-sub">zen / go</div></div>
<div class="metric"><div class="label"><span class="icon">🌐</span>代理健康</div><div class="value" id="m-proxy">-</div><div class="sub" id="m-proxy-sub">-</div></div>
<div class="metric"><div class="label"><span class="icon">🧠</span>模型</div><div class="value" id="m-models">-</div><div class="sub" id="m-models-sub">-</div></div>
<div class="metric"><div class="label"><span class="icon">📦</span>目录状态</div><div class="value" id="m-catalog">-</div><div class="sub" id="m-catalog-sub">-</div></div>
<div class="metric"><div class="label"><span class="icon">⏱</span>运行时长</div><div class="value" id="m-uptime">-</div><div class="sub">uptime</div></div>
</div>

<div class="section">
<div class="section-head"><span class="t">上游与偏好</span><span class="hint">upstream</span></div>
<div class="section-body"><table>
<tr><th style="width:120px">项目</th><th>值</th></tr>
<tr><td class="muted">prefer</td><td class="mono" id="o-prefer">-</td></tr>
<tr><td class="muted">zen 上游</td><td class="mono" id="o-zen">-</td></tr>
<tr><td class="muted">go 上游</td><td class="mono" id="o-go">-</td></tr>
<tr><td class="muted">配置路径</td><td class="mono" id="o-path">-</td></tr>
</table></div></div>
</div>

<!-- 代理与 Key -->
<div class="pane" id="pane-proxies">
<div class="section">
<div class="section-head"><span class="t">代理池</span><span class="hint" id="proxy-count">-</span></div>
<div class="section-body"><table>
<tr><th style="width:70px">#</th><th>代理</th><th style="width:110px">状态</th></tr>
<tbody id="proxy-rows"></tbody>
</table></div></div>

<div class="section">
<div class="section-head"><span class="t">Key 绑定</span><span class="hint">round-robin 绑定，失败自动迁移</span></div>
<div class="section-body"><table>
<tr><th style="width:70px">层级</th><th>Key</th><th style="width:100px">绑定代理</th><th style="width:80px">失败</th><th style="width:100px">冷却</th></tr>
<tbody id="node-rows"></tbody>
</table></div></div>
</div>

<!-- 模型目录 -->
<div class="pane" id="pane-models">
<div class="section">
<div class="section-head"><span class="t">模型目录</span><span class="hint" id="model-count">-</span></div>
<input class="search" id="model-search" placeholder="搜索模型…" autocomplete="off">
<div class="model-groups" id="model-groups"></div>
</div></div>

<!-- 配置 -->
<div class="pane" id="pane-config">
<div class="section">
<div class="section-head"><span class="t">config.json</span><span class="hint">保存自动备份 · 热重载无需重启</span></div>
<div class="editor-wrap">
<textarea id="cfg" spellcheck="false" placeholder="加载中…"></textarea>
<div class="editor-actions">
<button class="btn primary" id="btn-save">保存并热重载</button>
<button class="btn" id="btn-reload">仅重载</button>
<button class="btn" id="btn-discard">放弃修改</button>
<span class="hint" id="cfg-state"></span>
</div></div></div>
</div>

</div>
<div id="toasts"></div>

<script>
var rawJson = '';
function $(id){return document.getElementById(id)}
function toast(msg, kind){
  var t = document.createElement('div');
  t.className = 'toast ' + (kind || '');
  t.textContent = (kind === 'ok' ? '✅ ' : kind === 'bad' ? '❌ ' : '') + msg;
  $('toasts').appendChild(t);
  setTimeout(function(){ t.style.opacity = '0'; t.style.transition = 'opacity .3s'; setTimeout(function(){ t.remove(); }, 350); }, 3200);
}
function esc(s){return String(s).replace(/[&<>"']/g, function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c];})}
function j(url, opt){
  return fetch(url, opt).then(function(r){
    if (!r.ok) return r.text().then(function(t){ throw new Error(t.slice(0, 300)); });
    return r.json();
  });
}
function fmtUp(sec){
  if (!sec) return '-';
  var h = Math.floor(sec / 3600), m = Math.floor(sec % 3600 / 60), s = sec % 60;
  return (h ? h + 'h ' : '') + (m ? m + 'm ' : '') + s + 's';
}

/* ── Tab 切换 ── */
document.querySelectorAll('.tab').forEach(function(t){
  t.addEventListener('click', function(){
    document.querySelectorAll('.tab').forEach(function(x){ x.classList.remove('active'); });
    document.querySelectorAll('.pane').forEach(function(x){ x.classList.remove('active'); });
    t.classList.add('active');
    $('pane-' + t.dataset.tab).classList.add('active');
    if (t.dataset.tab === 'models') loadModels();
    if (t.dataset.tab === 'proxies') loadProxies();
  });
});

/* ── 总览 ── */
function loadStatus(){
  return j('/api/admin/status').then(function(s){
    var ok = s.ready;
    var b = $('readyBadge'), bt = $('readyText');
    b.className = 'badge ' + (ok ? 'ok' : 'bad');
    bt.textContent = ok ? 'READY' : 'DEGRADED';
    $('m-ready').textContent = ok ? '正常' : '异常';
    $('m-ready').className = 'value ' + (ok ? 'ok' : 'bad');
    $('m-ready-sub').textContent = s.version;
    $('m-keys').textContent = s.keys.total;
    $('m-keys-sub').textContent = 'zen ' + s.keys.zen + ' · go ' + s.keys.go;
    $('m-proxy').textContent = s.proxies.healthy + ' / ' + s.proxies.total;
    $('m-proxy').className = 'value ' + (s.proxies.healthy > 0 ? 'ok' : 'bad');
    $('m-proxy-sub').textContent = s.proxies.unhealthy > 0 ? s.proxies.unhealthy + ' 个异常' : '全部健康';
    $('m-models').textContent = s.models.exposed + ' <small>/ ' + s.models.total + '</small>';
    $('m-models-sub').textContent = 'zen ' + s.models.zen + ' · go ' + s.models.go;
    var ms = s.models.status, mcl = ms === 'ready' ? 'ok' : (ms === 'stale' ? 'warn' : 'bad');
    $('m-catalog').textContent = ms;
    $('m-catalog').className = 'value ' + mcl;
    $('m-catalog-sub').textContent = s.models.last_refresh ? new Date(s.models.last_refresh).toLocaleTimeString() : '未刷新';
    $('m-uptime').textContent = fmtUp(s.uptime_sec);
    $('ver').textContent = s.version;
    $('path').textContent = s.config_path;
    $('o-prefer').textContent = s.prefer;
    $('o-zen').textContent = s.upstream.zen;
    $('o-go').textContent = s.upstream.go;
    $('o-path').textContent = s.config_path;
  }).catch(function(e){ toast('状态加载失败: ' + e.message, 'bad'); });
}

/* ── 代理与 Key ── */
function loadProxies(){
  j('/api/admin/proxies').then(function(p){
    $('proxy-count').textContent = p.items.length + ' 个代理';
    var html = '';
    p.items.forEach(function(x){
      html += '<tr><td class="muted mono">#' + x.index + '</td>' +
        '<td class="mono">' + esc(x.name) + '</td>' +
        '<td><span class="dot-lg ' + (x.healthy ? 'ok' : 'bad') + '"></span><span class="' + (x.healthy ? '' : 'muted') + '">' + (x.healthy ? '健康' : '异常') + '</span></td></tr>';
    });
    $('proxy-rows').innerHTML = html || '<tr><td colspan="3" class="empty">无代理</td></tr>';
  }).catch(function(e){ toast('代理加载失败: ' + e.message, 'bad'); });

  j('/api/admin/nodes').then(function(n){
    var html = '';
    n.items.forEach(function(x){
      var fcls = x.failures === 0 ? 'fail-0' : (x.failures < 3 ? 'fail-1' : 'fail-2');
      var cool = x.cooldown_secs > 0 ? x.cooldown_secs + 's' : '-';
      html += '<tr><td><span class="tag ' + x.tier + '">' + x.tier + '</span></td>' +
        '<td class="mono">' + esc(x.key) + '</td>' +
        '<td><span class="tag proxy">proxy #' + x.proxy_index + '</span></td>' +
        '<td class="mono ' + fcls + '">' + x.failures + '</td>' +
        '<td class="mono muted">' + cool + '</td></tr>';
    });
    $('node-rows').innerHTML = html || '<tr><td colspan="5" class="empty">无 Key</td></tr>';
  }).catch(function(e){ toast('Key 加载失败: ' + e.message, 'bad'); });
}

/* ── 模型目录 ── */
var allModels = { zen: [], go: [] };
function loadModels(){
  j('/api/admin/models').then(function(m){
    allModels = m;
    $('model-count').textContent = (m.zen.length + m.go.length) + ' 个模型 · zen ' + m.zen.length + ' · go ' + m.go.length;
    renderModels('');
  }).catch(function(e){ toast('模型加载失败: ' + e.message, 'bad'); });
}
function renderModels(q){
  q = (q || '').toLowerCase();
  var html = '';
  [['zen', 'Zen 目录', allModels.zen], ['go', 'Go 目录', allModels.go]].forEach(function(g){
    var list = g[2].filter(function(x){ return !q || x.toLowerCase().indexOf(q) !== -1; });
    html += '<div class="model-group"><div class="ghead"><span class="tag ' + g[0] + '">' + g[0] + '</span>' + g[1] +
      ' <span class="count">' + list.length + ' / ' + g[2].length + '</span></div><div class="chips">';
    list.forEach(function(x){ html += '<span class="chip">' + esc(x) + '</span>'; });
    if (!list.length) html += '<span class="muted" style="font-size:12px">无匹配</span>';
    html += '</div></div>';
  });
  $('model-groups').innerHTML = html;
}
$('model-search').addEventListener('input', function(){ renderModels(this.value); });

/* ── 配置 ── */
function fetchCfg(){
  $('cfg-state').textContent = '加载中…';
  j('/api/admin/config').then(function(c){
    rawJson = c.raw_json;
    $('cfg').value = rawJson;
    $('cfg-state').textContent = '已加载 · 修改后点保存';
  }).catch(function(e){ $('cfg-state').textContent = '加载失败: ' + e.message; });
}
function saveCfg(){
  var v = $('cfg').value;
  try { JSON.parse(v); } catch (e) { toast('JSON 语法错误: ' + e.message, 'bad'); return; }
  $('btn-save').disabled = true;
  j('/api/admin/config', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ json: v }) })
    .then(function(r){ toast(r.status || '已保存并热重载', 'ok'); loadStatus(); })
    .catch(function(e){ toast(e.message, 'bad'); })
    .finally(function(){ $('btn-save').disabled = false; });
}
function reloadCfg(){
  $('btn-reload').disabled = true;
  j('/api/admin/reload', { method: 'POST' })
    .then(function(r){ toast(r.status || '已热重载', 'ok'); loadStatus(); })
    .catch(function(e){ toast(e.message, 'bad'); })
    .finally(function(){ $('btn-reload').disabled = false; });
}
$('btn-save').addEventListener('click', saveCfg);
$('btn-reload').addEventListener('click', reloadCfg);
$('btn-discard').addEventListener('click', fetchCfg);

/* ── 启动 ── */
loadStatus();
fetchCfg();
setInterval(loadStatus, 10000);
</script></body></html>`
