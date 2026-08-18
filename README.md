# OpenCode Farm

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
![Go](https://img.shields.io/badge/Language-Go-00ADD8)
![Ubuntu](https://img.shields.io/badge/Platform-Ubuntu-E95420)

自托管的 OpenCode 免费模型聚合网关：把**多账号 key 池 × 多出口 IP** 的免费额度合并成一个大池子，对外提供标准 OpenAI / Anthropic 兼容 API（`http://127.0.0.1:8080/v1`）。装好后任何支持 OpenAI 的客户端（Claude Code、Cline、OpenWebUI、脚本等）填这个地址即可调用免费模型。

> 仅供个人技术研究与低流量自用。第三方服务条款与账号风险请自行评估，详见 [DISCLAIMER](docs/DISCLAIMER.md)。

## 技术机制（为什么能用）

**额度模型**：OpenCode 免费模型按「key（账号）× 出口公网 IP」两个维度分别计额度——每个 IP 每天约 300~766 次，UTC 午夜重置，新 IP 有高配额宽限期；账号另有 5 小时冷却。

**池化思路**：

1. **多 key 池**：`zen_keys` / `go_keys` 分组管理多个账号 key，请求自动均衡选择、失败切换；
2. **多出口代理池**：egress 托管机场/住宅等线路，虚拟出多个隔离账号（`Default.user_N`、`residential.user_N`），每个 IP 一个独立额度池；
3. **1:1 Key-IP 粘性绑定**：同一 key 固定绑定同一出口 IP，避免频繁换 IP / 共用 IP 触发关联风控（`Throttling.BurstRate`）；
4. **客户端兼容**：网关自动注入官方客户端特征请求头（`User-Agent: opencode/…`、`x-opencode-session/request/project`），匹配官方客户端识别规则；
5. **网关兜底**：连接失败 / 认证失败 / 限流 / 5xx 自动切换节点与 key，带冷却退避；会话按哈希粘性路由，节点故障自动迁移。

**对外能力**：

- OpenAI：`/v1/models`、`/v1/chat/completions`、`/v1/responses`；Anthropic：`/v1/messages`
- 普通响应 + SSE 流式；文本 / 图片 / reasoning / 工具调用转换
- 健康检查 `/healthz`（无 key，返回模型、key 池、代理池汇总状态）

## 快速开始

数据目录放仓库同级：

```bash
cp .env.example .env                 # 按需改
export OPENCODE_FARM_DATA_DIR=/path/to/opencode-farm-data
bin/farm doctor                      # 环境检查
bin/farm install-egress              # 安装代理池原生二进制
bin/farm egress                      # 启动出口代理池 (127.0.0.1:2260)
bin/farm gateway                     # 启动 API 网关   (127.0.0.1:8080)
```

三个 curl 验证：

```bash
curl http://127.0.0.1:8080/healthz                     # 期望 ready:true, proxies.healthy=N
curl -H "Authorization: Bearer <你的本地key>" http://127.0.0.1:8080/v1/models
curl -X POST http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer <你的本地key>" -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash-free","messages":[{"role":"user","content":"hi"}]}'
```

可注册为 systemd 服务开机自启（模板见 `deploy/`，含启动顺序与资源限制）。

## 配置

真实配置放 `gateway/config.json`（不入库；示例见 `services/gateway/config.example.json`）：

- `zen_keys` / `go_keys`：官方 key 池（可多个，自动轮换）
- `server_keys`：本地调用 key（仅本机鉴权，不向上游转发）
- `proxies`：出口代理列表（`direct` / `http://` / `socks5://`，或指向本机 egress 的隔离账号）
- `upstream`：上游地址，默认 `https://opencode.ai/zen`（免费池）与 `/zen/go`（Go 池）
- `prefer`：模型同时存在两池时的优先上游（`zen` / `go`）
- `models.refresh_seconds`：模型目录刷新间隔；`performance`：连接池与超时；`retry`：最大尝试与总超时

## 常见问题

| 报错 | 含义 | 处理 |
|---|---|---|
| 403 requires explicit opt in | 该 key 的工作区未开通 Opt-in | 官方工作区页面勾选一次 |
| 429 5-hour usage limit reached | 账号 5 小时免费额度打满 | 等待冷却或增加 key |
| 429 Throttling.BurstRate | IP+key 关联风控 | 确认走独立出口，勿共用 IP |
| 502 all upstream attempts failed | key 池全部不可用 | 逐个验证 key 状态 |
| healthz: no_healthy_proxies | 出口池为空 | 检查 egress 日志是否为 Loaded 0 subscriptions |

## 目录结构

```text
services/gateway   网关源码（Go）
services/egress    出口代理池工具与维护脚本
services/client    客户端接入配置模板
tools/             网关 / egress / 网络维护工具
deploy/            systemd 服务模板 + 部署手册
docs/              玩法剖析、住宅线路研究、免责声明
```

## License

[MIT](LICENSE) © 2026 JIaDE-YX
