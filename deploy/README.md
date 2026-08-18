# systemd 服务模板（Ubuntu 原生运行）

opencode-farm 由两个 systemd 服务组成，随系统开机自启：

  opencode-farm-egress.service   出口代理池（Resin）  127.0.0.1:2260  必须先于 gateway 启动
  opencode-farm-gateway.service  统一 API 网关（Go）  127.0.0.1:8080  依赖 egress（Requires + 等待 2260 就绪）

## 安装

复制 *.service 到 /etc/systemd/system/ 并替换占位符：

  占位符          含义                        示例
  <REPO_ROOT>     仓库绝对路径                /home/you/opencode-farm
  <DATA_DIR>      数据目录绝对路径            /home/you/opencode-farm-data
  <USER>          运行服务的 Linux 用户名     you
  <HOME_DIR>      用户主目录                  /home/you
  <GO_BIN>        Go 可执行文件目录          /usr/local/go/bin（command -v go 查询）

    sudo cp deploy/opencode-farm-egress.service deploy/opencode-farm-gateway.service /etc/systemd/system/
    # 替换占位符
    sudo systemctl daemon-reload
    sudo systemctl enable --now opencode-farm-egress.service
    sudo systemctl enable --now opencode-farm-gateway.service

## 启动顺序保证（重要）

- gateway 声明 Requires=opencode-farm-egress.service + After=，systemd 会先把 egress 拉起来；
- gateway 的 ExecStartPre 等待 127.0.0.1:2260 可用（最多 30s）才真正启动，避免启动初期代理探测全部失败。

冷启动模拟验证：

    systemctl stop opencode-farm-gateway opencode-farm-egress
    systemctl start opencode-farm-gateway      # 会自动先拉起 egress
    sleep 30 && curl -s http://127.0.0.1:8080/healthz   # 期望 ready:true, proxies 9/9

## 真实数据目录（切勿指向默认路径）

egress 二进制（Resin）未设置 RESIN_STATE_DIR / RESIN_CACHE_DIR / RESIN_LOG_DIR 时会用
/var/lib/resin 等默认路径并创建全新空库（0 订阅 / 0 节点），导致网关代理全灭 ——
2026-08-19 已踩坑，务必指向数据目录：

    <DATA_DIR>/proxy/data/state   # subscriptions / platforms（state.db）
    <DATA_DIR>/proxy/data/cache   # 节点缓存、country.mmdb（cache.db）
    <DATA_DIR>/proxy/data/log     # 指标与请求日志（metrics.db / request_logs-*.db）

网关配置在 <DATA_DIR>/gateway/config.json（key 池、代理、限流参数）。
本机实例的真实接入信息（含 server key、模型清单）与已安装的 systemd 单元见 deploy/installed/README.md ——
该仓库为私人仓库（不公开），如仓库任何时刻被设为公开请第一时间移除 deploy/installed/ 并轮换 key。

## 资源护栏（2026-08-19 调优，低流量家用部署）

  服务     MemoryMax  CPUWeight  IOWeight  说明
  gateway  96M        80         80        峰值约 10-15MB，上限防泄漏
  egress   128M       20         40        峰值约 40MB，后台低优先级让出 CPU

另：/etc/systemd/journald.conf 设 SystemMaxUse=32M 控制日志磁盘占用（48M -> 24M）。

## 常用运维

    systemctl status opencode-farm-egress opencode-farm-gateway   # 状态
    journalctl -u opencode-farm-gateway -f                        # 网关日志
    journalctl -u opencode-farm-egress --since "5 minutes ago"    # egress 日志
    curl http://127.0.0.1:8080/healthz                            # 健康检查

排障速查：/healthz 报 no_healthy_proxies 时，先看 egress 启动日志是否 Loaded 0 subscriptions
（多半是数据目录指错，见上文）；住宅线路失效（端口轮换 / 套餐到期）时用 tools/egress/ 或
services/egress/import-proxies.js 重建订阅。
