# OpenCode Farm

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
![Language: Go](https://img.shields.io/badge/Language-Go-00ADD8)
![Platform: Ubuntu](https://img.shields.io/badge/Platform-Ubuntu-E95420)
![API: OpenAI/Anthropic Compatible](https://img.shields.io/badge/API-OpenAI%20%7C%20Anthropic-412991)

> **免责声明**：本仓库为**个人技术研究与学习用途**的开源项目。使用本项目访问第三方服务（如 OpenCode zen 免费模型）可能违反该服务的服务条款，存在**账号风控 / 封号风险**，请仅在合规、低流量、自用场景下使用，自行承担全部后果。本项目按现状（AS-IS）提供，无任何担保。详见 [docs/DISCLAIMER.md](docs/DISCLAIMER.md)。

## 目录

- [这是什么？](#这是什么)
- [有什么用？](#有什么用解决什么问题)
- [主要特性](#主要特性)
- [怎么用？](#怎么用)
- [构建与测试](#构建与测试)
- [运维速查](#运维速查)
- [常见问题](#常见问题)
- [目录结构](#目录结构)
- [License](#license)

---

## 这是什么？

**OpenCode Farm** 是一套自托管套件：它把"OpenCode 的免费模型额度"整合成一个**标准的本地 API 网关**（OpenAI / Anthropic 兼容），让你可以用任何现成的 AI 客户端直接调用这些免费模型，就像调用 OpenAI API 一样。

架构示意：

```text
你的客户端 ──(OpenAI 兼容, :8080/v1)──► Farm 网关(Go) ──(HTTP 代理, :2260)──► Egress 出口池 ──► OpenCode zen 免费模型
```

它由三个逻辑层组成（对外无需区分）：

| 组件 | 作用 |
|---|---|
| **Gateway**（Go，:8080） | 统一 API 网关：key 池管理、模型路由、请求/流式转换、限流重试、代理绑定 |
| **Egress**（Resin，:2260） | 出口代理池：多 IP 隔离、住宅/机场线路绑定、故障迁移 |
| **Client** | 本地客户端接入层的配置模板（模型清单、密钥环境） |

## 有什么用？（解决什么问题）

OpenCode 的免费模型有这些限制，**单账号 + 单出口 IP 很快就被打满**：

1. **按出口 IP 计独立日额度**（约 300~766 次/天/IP，UTC 午夜重置，新 IP 有宽限期）；
2. **客户端指纹门控**：非官方客户端调用会被 403 / 极速限流；
3. **账号级限制**：每个工作区 5 小时免费额度（429），新账号需先 Opt-in（403）。

**opencode-farm 把这些限制"摊平"成一个大额度池**：

- 多账号 key 池 × 多出口 IP = **多个独立额度池叠加**，总量大幅提升；
- 自动注入官方请求头（User-Agent / x-opencode-session 等）= **绕过客户端指纹门控**；
- 1:1 Key-IP 粘性绑定 = **模拟不同国家的真实用户，避免关联风控**。

**对使用者来说，效果是**：把一批免费模型（deepseek-v4-flash-free、hy3-free、laguna-s-2.1-free、mimo-v2.5-free、nemotron-3-ultra-free、nemotron-3.5-lightning-free）通过一个本地地址 `http://127.0.0.1:8080/v1` 暴露出来，**任何支持 OpenAI API 的客户端**（Claude Code、Cline、Continue、ChatGPT-Next-Web、OpenWebUI、自写脚本、各种 SDK）填上这个地址就能用。

## 主要特性

- ✅ 标准 OpenAI 兼容：`/v1/models`、`/v1/chat/completions`、`/v1/responses`（另支持 Anthropic `/v1/messages`）
- ✅ 普通响应与 SSE 流式响应
- ✅ 多 key 池（Zen / Go 池分离）与自动均衡分配
- ✅ 代理池支持：直连、HTTP、HTTPS、SOCKS5；1:1 Key-IP 粘性绑定 + 故障自动迁移
- ✅ 请求协议自动转换（OpenAI ↔ Anthropic ↔ Responses）
- ✅ 会话粘性（稳定会话哈希）+ 节点故障回退 + key 冷却
- ✅ 健康检查 `/healthz` 与服务自愈（systemd Restart=always）

## 怎么用？

### 第 1 步：准备数据目录

数据目录放在仓库同级（真实 key/配置不入库）：

```text
/path/to/opencode-farm-data/
├── gateway/          # 网关真实配置（config.json）
├── proxy/            # egress token 和运行时数据（data/ 为实际运行目录）
└── client/           # 客户端配置模板（settings.yaml / .credentials.yaml）
```

复制 `.env.example` 为 `.env` 或直接设环境变量：

```bash
export OPENCODE_FARM_DATA_DIR=/path/to/opencode-farm-data
```

### 第 2 步：检查环境并安装依赖

环境要求：Ubuntu（或任意 Linux）、Go 1.24+、Node.js、curl。

```bash
bin/farm doctor          # 检查 Go/Node/curl 与数据目录
bin/farm install-egress  # 安装 egress 原生二进制
make install
```

### 第 3 步：准备好你的凭证

在数据目录的 `gateway/config.json` 中配置（示例见 `services/gateway/config.example.json`）：

- `zen_keys` / `go_keys`：你的 OpenCode 官方 API key 列表（可多个）；
- `server_keys`：**本地调用 key**（自己生成一串字符串，只在本机有效，不会发给上游）；
- `proxies`：出口代理列表（`direct`、`http://`、`socks5://`，或指向本机 egress 的账号）；
- `upstream`：上游地址，默认 `https://opencode.ai/zen`（免费池）与 `https://opencode.ai/zen/go`。

### 第 4 步：启动

```bash
bin/farm egress     # 出口代理池，127.0.0.1:2260
bin/farm gateway    # API 网关，127.0.0.1:8080
```

（可注册为 systemd 服务开机自启，模板见 `deploy/`。）

### 第 5 步：在你的客户端里接入

任何 OpenAI 兼容客户端，填：

```text
Base URL : http://127.0.0.1:8080/v1
API Key  : <server_keys 里你设置的本地 key>
模型     : deepseek-v4-flash-free 等（/v1/models 实时返回）
```

命令行快速验证（curl）：

```bash
# 1. 健康检查（无需 key）
curl http://127.0.0.1:8080/healthz
# {"status":"ok","ready":true,"proxies":{"healthy":N}}

# 2. 模型列表
curl -H "Authorization: Bearer <local-key>" http://127.0.0.1:8080/v1/models

# 3. 一句话对话
curl -X POST http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer <local-key>" -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash-free","messages":[{"role":"user","content":"hi"}]}'
```

## 构建与测试

Gateway 是独立 Go 模块，可单独编译：

```bash
cd services/gateway
go build -trimpath -o opencode-farm .       # 编译
go vet ./...                                 # 静态检查
go test ./...                                # 单元测试（当前无自带测试文件时输出 no test files）
```

## 运维速查

```bash
bin/farm status                        # 网关 + egress 健康总览
systemctl status opencode-farm-gateway # systemd 方式（若已注册）
journalctl -u opencode-farm-gateway -f # 网关日志
```

## 常见问题

| 现象 | 含义 | 处置 |
|---|---|---|
| `403 RegionError: requires explicit opt in` | 该 key 的工作区未 Opt-in | 到 opencode 工作区页面勾选一次 |
| `429 GoUsageLimitError: 5-hour usage limit reached` | 5 小时免费额度打满 | 等倒计时或加 key/换账号 |
| `429 Throttling.BurstRate` | IP+key 关联风控 | 确认走 Egress 独立出口且 1:1 绑定 |
| `502 all upstream attempts failed` | 池内所有 key 不可用 | 逐个检查 key 状态 |
| healthz 报 `no_healthy_proxies` | 出口池空 | 看 egress 日志是否 `Loaded 0 subscriptions`，检查订阅/线路 |

## 目录结构

```text
opencode-farm/
├── bin/farm                # 统一管理入口
├── services/
│   ├── gateway/            # 网关源码（Go，可独立编译）
│   ├── egress/             # 出口代理池工具与维护脚本
│   └── client/             # 客户端接入配置模板
├── tools/                  # 网关/egress/网络维护工具
├── deploy/                 # systemd 服务模板 + 部署手册
└── docs/                   # 玩法说明、住宅研究、免责声明
```

## License

[MIT](LICENSE) © 2026 JIaDE-YX

## 致谢与说明

- 感谢 [LINUX DO](https://linux.do) 社区的技术思路支持。
- 本项目为自用而建，按现状提供（AS-IS）；请遵守目标服务条款与当地法律，勿大规模滥用。
