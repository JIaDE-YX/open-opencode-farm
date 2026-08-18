# OpenCode Zen 免费号池玩法说明（opencode-farm + Egress + Residential + client）

> 更新时间：2026-08-17
> 适用范围：Ubuntu 原生运行（不再依赖 Docker）
> 一句话总结：**多账号 key 池 × 多住宅出口 IP × 官方 UA 指纹伪装 × 1:1 绑定防关联 = 免费白嫖 opencode zen 免费模型**

---

## 1. 架构总览

```
┌────────────┐    OpenAI 兼容    ┌──────────────────┐   HTTP(S) 代理    ┌─────────────┐
│  client web   │ ────────────────► │  opencode-farm │ ────────────────► │    Egress    │
│  (浏览器)  │   :8080/v1        │   网关 (Go)      │   host:2260      │  代理网关    │
└────────────┘                   │   端口 8080      │                   │   端口 2260  │
     ▲                           └──────────────────┘                   └──────┬──────┘
     │  OPENCODE_LOCAL_API_KEY(<local-key>)                             │
     │                                                                         ▼
┌──────────────────────────────────────────────────────────────┐    Residential 动态住宅 SOCKS5
│  key 池（6 个 sk- 官方 key，来自多个 opencode 账号/工作区）     │    Default.user_1..3（机场）
│  1:1 绑定：KeyN 永远走同一出口 IP，模拟全球真实程序员            │    residential.user_1..6（住宅）
└──────────────────────────────────────────────────────────────┘
                                                                         │
                                                                         ▼
                                                        ┌──────────────────────────┐
                                                        │  opencode.ai/zen/v1      │
                                                        │  （只走 zen，免费模型）    │
                                                        └──────────────────────────┘
```

## 2. 核心机制（为什么能白嫖）

### 2.1 Per-IP 独立日额度池
- OpenCode 不只按 key 计费，还按**公网出口 IP** 建独立日调用计数（约 300~766 次/天/IP）。
- **新住宅 IP 有宽限期**，首次从新 IP 发请求时速率上限极高。
- UTC 午夜（北京时间 08:00）归零重置。
- 因此：多个住宅 IP = 多个独立额度池 = 数倍免费额度。

### 2.2 客户端指纹门控（必须伪装）
- 官方网关校验 `User-Agent` 等客户端指纹，非官方客户端直接 403 或极速限流。
- `opencode-farm` 自动注入 `User-Agent: opencode/...` 及 `x-opencode-session/request/project` 头，让请求看起来来自官方 CLI。

### 2.3 1:1 Key-IP 粘性绑定（防关联）
- 多 key 共用单 IP 或频繁换 IP → 触发 `Throttling.BurstRate` 风控。
- Egress（:2260）虚拟出多个隔离账户（`Default.user_N` / `residential.user_N`），每个 key 固定绑定一条独立出口，模拟不同国家的独立用户。

### 2.4 官方账号侧的两个门槛
1. **Opt-in（403 根源）**：新账号若未在网页工作区勾选同意（中国区托管模型），调用 `deepseek-v4-flash` 等会报 `403 RegionError: requires explicit opt in`。→ 需打开 `https://opencode.ai/workspace/<id>/go` 勾选一次。
2. **5 小时限额冷却（429 根源）**：每个工作区每 5 小时有免费额度，打满后 `429 GoUsageLimitError: 5-hour usage limit reached`，倒计时结束后自动恢复。→ 多账号轮换解决。

---

## 3. 组件清单

| 组件 | 形态 | 地址/端口 | 备注 |
|---|---|---|---|
| opencode-farm | Ubuntu 原生进程 | `127.0.0.1:8080` | 网关，对外 OpenAI/Anthropic 兼容 |
| Egress | Ubuntu 原生进程 | `127.0.0.1:2260` | 出口代理池 |
| client web | node 进程 | `127.0.0.1:3080` | 本地客户端 UI |
| Residential | 外部服务 | resXX.residential.info:PORT | 动态住宅 SOCKS5，2GB/3天，**2026-08-18 到期**（注意续费） |

### 3.1 项目与配置文件位置

| 路径 | 内容 |
|---|---|
| `opencode-farm-data/gateway/config.json` | **网关主配置**（key 池、代理、路由、重试） |
| `opencode-farm-data/gateway/config.json.bak*` | 历史配置备份 |
| `opencode-farm-data/client/settings.yaml` | client 模型/提供商配置 |
| `opencode-farm-data/client/.credentials.yaml` | client 密钥（含 `OPENCODE_LOCAL_API_KEY`） |
| `opencode-farm-data/proxy/.tokens.txt` | Egress token |
| `opencode-farm-data/docs/` | 敏感账号/线路档案 |

---

## 4. config.json 详解

当前有效配置（2026-08-17）：

```jsonc
{
  "prefer": "go",            // 模型同时存在 zen/go 时优先用 go；【只走 zen 可清空 go_keys】
  "zen_keys": [ /* 6 个 sk- key，来自多个工作区 */ ],
  "go_keys":  [ /* 与 zen_keys 相同 */ ],
  "upstream": {
    "go":  "https://opencode.ai/zen/go",   // Go 池（订阅/付费，26 个模型）
    "zen": "https://opencode.ai/zen"       // Zen 池（pay-per-use + 免费模型）
  },
  "retry": { "timeout_seconds": 300, "max_attempts": 5 },
  "listen": "127.0.0.1:8080",
  "server_keys": ["<local-key>"],   // 本地调用凭据
  "proxies": [
    "http://Default.user_1:px-...@host.docker.internal:2260",  // 前 3 条机场
    "http://residential.user_1:px-...@host.docker.internal:2260",  // 后 6 条住宅
    // ... 共 9 条
  ],
  "admin_password": "<admin-password>",
  "performance": {
    "failure_cooldown_seconds": 10,
    "connect_timeout_seconds": 5
  },
  "models": { "protocols": {}, "refresh_seconds": 300 }
}
```

关键字段：
- `server_keys`：调用本网关的本地 key（`<local-key>`），不会发给官方。
- `zen_keys` / `go_keys`：官方 key 池；模型同时存在于两池时按 `prefer` 选上游。
- `proxies`：出口代理列表，key 自动均衡绑定（亲和性）；节点失败自动迁移。
- `retry.max_attempts`：单请求最大尝试次数（含首次）；网络错误/认证失败/限流/5xx 会切换节点。

> ⚠️ 若只走 zen 免费模型：可将 `go_keys` 清空为 `[]`（README 要求两池至少一个非空，zen 非空即可），改后重启 gateway 进程生效。

---

## 5. 官方免费模型（zen 池，2026-08-17 实测）

在官方设置里关闭了 zen 付费模型后，`/zen/v1/models` 只返回以下模型：

| 模型 ID | 类型 | 实测 |
|---|---|---|
| `deepseek-v4-flash-free` | 免费 | ✅ 200 |
| `hy3-free` | 免费 | ✅ 200 |
| `laguna-s-2.1-free` | 免费 | ✅ 200 |
| `mimo-v2.5-free` | 免费 | ✅ 200 |
| `nemotron-3.5-lightning-free` | 免费 | ✅ 200 |
| `nemotron-3-ultra-free` | 免费 | ✅ 200 |
| `deepseek-v4-flash` / `deepseek-v4-pro` | 付费 | 需 opt-in，不免费使用 |

（社区早期另有 `mimo-v2-pro-free`、`nemotron-3-super-free`、`big-pickle`、`north-mini-code-free` 等，会随官方上下架变化，以 `/zen/v1/models` 实时返回为准。）

---

## 6. client 接入配置

### 6.1 `C:\Users\<用户名>\.client\.credentials.yaml`
```yaml
OPENCODE_LOCAL_API_KEY: <local-key>
```

### 6.2 `C:\Users\<用户名>\.client\settings.yaml`
```yaml
llm-pi-ai:
  providers:
    opencode-local:
      apiKeyEnv: OPENCODE_LOCAL_API_KEY
      api: openai-completions
      baseURL: http://127.0.0.1:8080/v1
      models:
        - id: deepseek-v4-flash-free
          name: deepseek-v4-flash-free
          contextWindow: 200000
          maxTokens: 128000
        - id: mimo-v2.5-free
          name: mimo-v2.5-free
          contextWindow: 200000
          maxTokens: 32000
        - id: nemotron-3-ultra-free
          name: nemotron-3-ultra-free
          contextWindow: 1000000
          maxTokens: 128000
        - id: nemotron-3.5-lightning-free
          name: nemotron-3.5-lightning-free
          contextWindow: 1000000
          maxTokens: 128000
        - id: hy3-free
          name: hy3-free
          contextWindow: 256000
          maxTokens: 128000
        - id: laguna-s-2.1-free
          name: laguna-s-2.1-free
          contextWindow: 1000000
          maxTokens: 128000
agent-default-model:
  provider: opencode-local
  model: deepseek-v4-flash-free
```

> 注意：`settings.yaml` 为热加载，改完无需重启 client web；若未生效可重启（`client web`，默认端口 3080）。

---

## 7. 健康检查与测试

```powershell
# 1. 网关健康总览（无需 key）
curl http://127.0.0.1:8080/healthz
# 期望：{"status":"ok","ready":true, ... "proxies":{"healthy":9}}

# 2. 模型列表（带本地 key）
curl -H "Authorization: Bearer <local-key>" http://127.0.0.1:8080/v1/models

# 3. 单模型对话测试
$body = '{"model":"deepseek-v4-flash-free","messages":[{"role":"user","content":"hi"}],"max_tokens":5}'
curl -s -X POST http://127.0.0.1:8080/v1/chat/completions -H "Authorization: Bearer <local-key>" -H "Content-Type: application/json" -d $body

# 4. 容器状态
docker ps | Select-String "egress|opencode-farm"
# 5. 看日志
docker logs opencode-farm-opencode-farm-1 --tail 50
docker logs egress --tail 50
```

---

## 8. 常见错误码与处置

| 现象 | 含义 | 处置 |
|---|---|---|
| `403 RegionError: requires explicit opt in` | 该 key 的工作区未勾选 Opt-in | 打开 `https://opencode.ai/workspace/<id>/go` 勾选一次 |
| `429 GoUsageLimitError: 5-hour usage limit reached` | 该工作区 5 小时免费额度打满 | 等倒计时；或加号/换号 |
| `429 Throttling.BurstRate` | IP+key 关联风控 | 确认走 Egress 住宅出口且 1:1 绑定，勿共用 IP |
| `401 Invalid API key` | key 被官方注销/删除 | 后台重新生成，替换 config.json 对应项 |
| `502 all upstream attempts failed` | 池内所有 key 均不可用 | 用上面测试脚本逐个查 key 状态 |
| `Bad Gateway / EOF / timeout` | Egress 链路或代理节点问题 | `docker logs egress` 看 probe；检查 Residential 流量/到期 |
| HTTP 000（curl） | 连接超时 | 该代理节点可能不可用，网关会自动迁移绑定 |

---

## 9. 日常维护

### 9.1 常用命令
```powershell
docker restart opencode-farm-opencode-farm-1   # 重启网关（config 改动后）
docker restart egress                            # 重启代理池
docker logs -f opencode-farm-opencode-farm-1   # 跟踪网关日志
```

### 9.2 换 key / 加号流程
1. 官方 `opencode.ai` 新建账号 → 工作区 `/go` 页面先勾选 Opt-in；
2. 生成 `sk-` key，追加进 `config.json` 的 `zen_keys`（和 `go_keys`，若仍走 go）；
3. `docker restart opencode-farm-opencode-farm-1`；
4. `curl /healthz` 确认 key 计数与模型数正常。

### 9.3 时间节点
- **UTC 午夜（北京 08:00）**：per-IP 日额度归零。
- **5 小时冷却**：从打满时刻起算，官方报错里会带 `Resets in Xh Ymin`。
- **Residential 套餐**：2GB/3天，**2026-08-18 到期**，到期前续费，否则住宅线路全部失效。

---

## 10. 注意事项

- 本玩法涉及绕过官方免费额度限制与多账号使用，**违反 opencode 服务条款，有封号/风控风险**，仅供个人低强度自用。
- 免费模型会收集数据用于模型改进，**勿发送敏感代码/密钥**。
- `config.json`、`residential-farm-archive.md`、`.credentials.yaml` 含真实凭证，**勿外传、勿提交 git**。
- Egress 的 latency probe 偶发失败属正常（部分线路/目标被墙），以网关实际转发结果为准；若大量失败再排查线路。

---

## 附录：本次排障时间线（2026-08-17）

1. 用户误删旧 key → 旧 key `401 Invalid`，官方模型数从 72 降到 32（zen 8 + go 26，因官方关闭付费模型展示）。
2. 重新生成 6 个 key 入池 → 逐 key 直连 `/zen/go/v1/models` 与 `/zen/v1/models` 全部 200。
3. 经网关实测 6 个 zen 免费模型全部 200 ✅；`deepseek-v4-flash`（付费）403 需 opt-in（属预期，未开启）。
4. 结论：**池子健康，问题仅在"个别 key 的工作区未 opt-in"或"5 小时冷却"，均随官方状态自动恢复**；client 配置好后即可正常使用。

---

## 附录 2：维护记录（2026-08-19）

> 本次记录包含三层变更：**egress 修复、kookeey 线路重建、性能优化与开机自启动**。文中所有真实密钥一律用占位符，实际值只存在于 opencode-farm-data/ 中。

### 1. egress 数据目录丢失修复（根因）

**现象**：网关 /healthz 503 且 proxies 9/9 unhealthy、模型目录刷不出来；egress 反复重启失败（190+ 次循环）。

**根因**：systemd 里 egress 服务直接运行二进制且**未设置数据目录环境变量**，Resin 落到默认路径 /var/lib/resin、/var/cache/resin 建了全新空库（0 订阅 / 0 节点），而真实数据一直在 opencode-farm-data/proxy/data/（2 订阅 / 27 节点）。

**修复**：
- /etc/systemd/system/opencode-farm-egress.service 增加 RESIN_STATE_DIR / RESIN_CACHE_DIR / RESIN_LOG_DIR 指向 proxy/data/；
- 同步更新仓库 deploy/opencode-farm-egress.service 模板（防止重装复发）。

**验证**：启动日志 Loaded 2 subscriptions / 27 static nodes，网关恢复 ready。

### 2. kookeey 住宅线路重建（本次最重要）

**背景**：kookeey 套餐**未过期**。真正原因是 egress 中 residential 节点指向的网关域名 gate-hk.kookeey.info:1000（旧订阅 content 里的 socks5:// 网关地址）**已被服务商下线**（公共 DNS 返回 NXDOMAIN），且 kookeey 平台配置在历史重建中丢失，导致 kookeey.user_1..6 全部 404。

**关键结论（DNS 排查）**：
- 部分网络环境（如 Windows 宿主 fake-ip / mihomo DNS 劫持）会返回假地址（198.18.0.0/15 网段），判断域名是否真实存在必须借道代理走公共 DoH（dns.google / cloudflare-dns.com）；
- 必须借道代理走公共 DoH（dns.google / cloudflare-dns.com）判断域名真伪；
- resXX.kookeey.info 直连子域**仍然有效**（res25 -> 148.153.211.86 等），直连认证格式 <kookeey-policy-account>:<password> 可用（凭证见 opencode-farm-data/docs 档案，勿外传/勿提交 git）。

**动作**（通过 egress Admin API，不动 config.json）：
1. 备份旧 residential 订阅，新建订阅 residential-kookeey（content 为 socks5://user:pass@resXX:PORT 逐行格式，**必须是带 scheme 的 URL 行**，裸 user:pass@host:port 解析器不支持）；
2. 创建 kookeey 平台（regex_filters: ["residential-kookeey"]）；
3. 重启网关触发重新探测。

**验证**：10 条直连线路实测出站成功（英/美/日/港等住宅 IP）；kookeey.user_1..6 每账号独立住宅出口；网关 /healthz proxies 9/9 healthy。

**注意**：kookeey 动态住宅端口会轮换，线路失效时复跑验证脚本后更新订阅即可（当前约 10/19 条可用）。

### 3. 开机自启动

- 两个服务 systemctl enable 已启用（挂在 multi-user.target）；
- gateway unit 补齐 Requires=opencode-farm-egress + After + ExecStartPre（等待 127.0.0.1:2260 就绪，最多 30s），保证开机顺序：egress 先起，gateway 后起；
- 冷启动模拟验证通过（systemd 自动按依赖顺序拉起，30s 内 /healthz ready）。

### 4. 性能优化（降低后台占用）

| 项 | 优化前 | 优化后 |
|---|---|---|
| 网关空闲连接池 | 2048 | 256 |
| 每主机空闲连接 | 256 | 32 |
| 空闲连接超时 | 120s | 60s |
| 模型目录刷新 | 300s | 900s（stale 阈值同步 1800s） |
| egress cgroup 内存 | 约 40MB | 约 26MB |
| 内存上限保护 | 无 | gateway 96M / egress 128M |
| CPU/IO 权重 | 默认 | egress 20/40（后台低优先），gateway 80/80 |
| journald 磁盘 | 48M | 24M（SystemMaxUse=32M） |

改动文件：opencode-farm-data/gateway/config.json（performance/models.refresh_seconds，备份于 backups/）、两个 systemd unit、/etc/systemd/journald.conf。

### 5. 客户端接入信息（占位）

- Base URL：http://127.0.0.1:8080/v1（OpenAI 兼容）
- API Key：<local-key>（实际值在 opencode-farm-data/gateway/config.json 的 server_keys，仅本机有效）
- 模型：deepseek-v4-flash-free / hy3-free / laguna-s-2.1-free / mimo-v2.5-free / nemotron-3-ultra-free / nemotron-3.5-lightning-free（付费的 deepseek-v4-flash、deepseek-v4-pro 需 Opt-in，一般不可用）
