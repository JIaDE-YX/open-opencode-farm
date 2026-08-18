# OpenCode Farm

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
![Go](https://img.shields.io/badge/Language-Go-00ADD8)
![Ubuntu](https://img.shields.io/badge/Platform-Ubuntu-E95420)

**说白了，这玩意儿就是薅 OpenCode 免费模型额度用的。**

官方的免费额度是**按账号和出口 IP 分开算**的：一个号、一个 IP，一天也就几百次调用。农场干的事就是——攒一堆账号的 key、挂一堆出口线路，把每天的免费额度凑到一起，然后打包成一个标准 OpenAI 接口（`http://127.0.0.1:8080/v1`）。

装完后你手里任何支持 OpenAI 接口的客户端（Claude Code、Cline、OpenWebUI、脚本都行）填这个地址就能调免费模型。号开多少、线路挂多少，自己看着办，风险自负。

> 仅供个人技术学习与低流量自用。第三方服务条款风险和账号风险自行评估，详见 [DISCLAIMER](docs/DISCLAIMER.md)。

## 快速开始

数据目录放仓库同级：

```bash
cp .env.example .env                 # 按需改
export OPENCODE_FARM_DATA_DIR=/path/to/opencode-farm-data
bin/farm doctor                      # 环境检查
bin/farm install-egress              # 装代理池二进制
bin/farm egress                      # 起代理池 (127.0.0.1:2260)
bin/farm gateway                     # 起网关   (127.0.0.1:8080)
```

三个 curl 验证：

```bash
curl http://127.0.0.1:8080/healthz                     # 健康：ready:true probes:healthy:N
curl -H "Authorization: Bearer <你的本地key>" http://127.0.0.1:8080/v1/models
curl -X POST http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer <你的本地key>" -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash-free","messages":[{"role":"user","content":"hi"}]}'
```

可注册 systemd 开机自启，模板在 `deploy/`。

## 配置

真实配置放 `gateway/config.json`（不入库，示例见 `services/gateway/config.example.json`）：

- `zen_keys` / `go_keys`：你的官方 key，可多个，自动轮换
- `server_keys`：本地调用 key，自己随便生成，只本机有效
- `proxies`：出口代理，`direct` 或 `socks5://...`，或指向本机 egress 的账号
- `upstream`：默认 `https://opencode.ai/zen`（免费池）

## 常见问题

| 报错 | 意思 | 怎么办 |
|---|---|---|
| 403 requires explicit opt in | 该 key 的工作区没开通 Opt-in | 去官方工作区页面勾一次 |
| 429 5-hour usage limit reached | 这个账号 5 小时额度用完 | 等会儿，或加 key |
| 429 Throttling.BurstRate | 风控了 | 确认走独立出口，别共用 IP |
| 502 all upstream attempts failed | key 全挂了 | 逐个检查 key |
| no_healthy_proxies | 出口池空了 | 看 egress 日志是不是 Loaded 0 subscriptions |

## 目录结构

```text
services/gateway   网关源码 (Go)
services/egress    代理池工具
services/client    客户端配置模板
tools/             维护脚本
deploy/            systemd 模板 + 部署说明
docs/              玩法与免责声明
```

## License

[MIT](LICENSE) © 2026 JIaDE-YX
