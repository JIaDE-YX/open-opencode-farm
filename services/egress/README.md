# Egress 出口代理池

出口代理池负责多 IP 隔离、住宅/机场线路绑定和故障迁移。

## 原生运行（Ubuntu）

```bash
bin/farm install-egress
bin/farm egress
```

默认监听 `127.0.0.1:2260`。

## 数据目录

真实 token 和运行时数据不存放在 Git 仓库中，位于：

```text
opencode-farm-data/proxy/
├── .tokens.txt         # RESIN_ADMIN_TOKEN / RESIN_PROXY_TOKEN
└── data/
    ├── cache/
    ├── state/
    └── log/
```

## 导入线路

```bash
node services/egress/import-proxies.js <线路文件> [订阅名] [--skip-test]
```

## 参考

- `deploy/legacy/container-egress.example.yml` 保留容器部署方式，仅作参考
