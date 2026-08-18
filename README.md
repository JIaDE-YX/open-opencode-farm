# OpenCode Farm

> ⚠️ **声明（免责）**：本仓库为**个人技术研究与学习用途**的开源项目。使用本项目访问第三方服务（如 OpenCode zen 免费模型）可能违反该服务的服务条款与使用政策，存在**账号风控 / 封号风险**，请仅在合规、低流量、自用场景下使用，并自行承担全部后果。本项目不附带任何担保。详见 [docs/DISCLAIMER.md](docs/DISCLAIMER.md)。

OpenCode Farm 是一套在 Ubuntu 上原生运行的多账号 OpenCode 免费模型接入系统，对外提供统一的 OpenAI/Anthropic 兼容 API。

它由三个逻辑层组成，但对外不需要区分它们：

- **Gateway**：统一 API 网关，负责 key 池、模型路由、请求转换和限流处理
- **Egress**：统一出口代理池，负责多 IP 隔离、住宅节点绑定和故障迁移
- **Client**：本地客户端接入层，提供模型配置和密钥环境

## 快速开始

### 1. 准备数据目录

数据目录默认放在仓库同级目录：

```text
/path/to/opencode-farm-data/
├── gateway/          # 网关真实配置（config.json）
├── proxy/            # 代理池 token 和运行时数据（data/ 为实际运行目录）
└── client/           # 客户端配置（settings.yaml / .credentials.yaml）
```

可复制 `.env.example` 为 `.env` 后修改；或直接设置环境变量：

```bash
export OPENCODE_FARM_DATA_DIR=/path/to/opencode-farm-data
```

### 2. 检查环境

```bash
bin/farm doctor
```

需要：

- Go 1.24+（编译 gateway）
- Node.js（运行维护脚本）
- curl（健康检查）

### 3. 安装原生依赖

```bash
bin/farm install-egress
make install
```

### 4. 启动服务

```bash
bin/farm egress     # 出口代理池，默认 127.0.0.1:2260
bin/farm gateway    # 统一 API 网关，默认 127.0.0.1:8080
```

也可以使用：

```bash
make egress
make gateway
```

### 5. 验证

```bash
bin/farm status
curl http://127.0.0.1:8080/healthz
```

## 目录结构

```text
opencode-farm/
├── bin/
│   └── farm                 # 统一管理入口
├── services/
│   ├── gateway/             # 网关源码
│   ├── egress/              # 出口代理池（原生二进制 + 可选容器参考）
│   └── client/              # 客户端接入层配置模板
├── tools/
│   ├── gateway/             # 网关维护工具
│   ├── egress/              # 出口代理池维护工具
│   ├── network/             # 外部线路/住宅网络维护工具
│   └── client/              # 客户端测试工具
├── docs/                    # 架构、玩法与排障文档
└── Makefile                 # 常用命令入口
```

## 说明

- 真实密钥、运行时数据库和账号档案已移出 Git，存放在 `opencode-farm-data/`
- 默认部署方式为 Ubuntu 原生进程，不再依赖 Docker
- 如需容器方式部署，`services/gateway/compose.yaml` 和 `deploy/legacy/container-egress.example.yml` 仍保留作为参考
