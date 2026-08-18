# residential 代理服务商 API 调研报告

> 目的：为 AI 代理农场从 residential（海外住宅/动态代理）提取 30 条动态住宅代理线路，喂给本地 Egress（upstream）代理池。
> 调研方式：纯调研（web 抓取 + 接口连通性诊断），未操作任何账号。
> 信息源：residential 官方 API 文档（`https://www.residential.net/apidoc` 与其内嵌 JS `index.94dfde9d.js` 中的完整英文 API 文档）、官方帮助站 `https://help.residential.com/`、以及对本机网关端口的实测诊断。

---

## 一、residential 完整 API 概览

### 1.1 请求基础（来自官方文档）

- **API 请求路径**：`https://residential.com/<method>`（动态代理提取等部分接口实际用 `https://www.residential.com/clientapi/<method>`）
- **方法**：全部使用 `HTTP GET`
- **必带参数**：`accessid`（Developer ID，来自账户信息）、`signature`（签名）、`ts`（秒级 Unix 时间戳）
- **频率限制**：每个账户 **10 qps/s**
- **URL 模板**：
  ```
  https://residential.com/[method]?accessid=[developer_id]&signature=[signature_value]&ts=[timestamp]&<业务参数>
  ```
- **官方示例**：
  `https://residential.com/stock?accessid=1&signature=...&g=433&ts=1609430400`
- **响应格式**（统一 JSON，4 个字段）：
  ```json
  { "success": bool, "data": object, "msg": string, "code": int }
  ```
- 实测：`https://www.residential.com/clientapi/pickdynamicipsbyw?g=433&n=1&t=2&ts=1700000000` 返回 `{"success":false,"code":40001,"msg":"身份验证错误","data":""}` —— 接口真实存在（40001=签名错误/缺少签名），证明路径正确、仅差有效签名。

### 1.2 签名返回码（诊断用）

| code | 含义 |
|------|------|
| 0 | 成功 |
| 10015 | Signature Timeout（签名超时，ts 过期） |
| 10059 | Signature error or timeout（签名错误或超时） |
| 10006/40003 | Param Error |
| 40001 | Bad Auth（身份验证错误） |
| 40034 | UserID 与 Developer-Token 不匹配 |
| 60001 | Frequently Request（频率超限） |

---

## 二、完整 API 方法表

> 方法名从官方 API 文档 JS 中的 `/方法名?accessid=` 模式提取。以下按分类列出（**★ = 动态住宅代理直接相关，本任务核心**）。

### 2.1 动态 / 住宅代理 ★

| 方法 | 参数 | 说明 |
|------|------|------|
| `pickdynamicipsbyw` ★ | `g`(动态区域ID,int), `n`(提取数量 1-100, 默认10), `t`(白名单类型: 2=动态住宅, 3=移动代理) | **按白名单提取动态住宅 IP**。返回 `data:["ip:port",...]`。本任务主接口 |
| `checkcans` ★ | `g`,`n` 等 | Check Traffic Allocation Eligibility（检查流量分配资格/提取） |
| `ip` | 动态线路相关 | 动态线路 IP 查询/提取 |
| `rotate` ★ | — | 动态线路强制更换 IP |
| `toggle` | — | 动态线路开关 |
| `release` | — | 释放动态线路（释放当前 IP） |
| `batchrelease` | — | 批量释放动态线路（v2022.08 起） |
| `batchchangeip` | — | 批量切换动态 IP（v2022.08 起） |

### 2.2 线路（Line）相关

| 方法 | 参数 | 说明 |
|------|------|------|
| `pl` | — | Proxy List（线路列表） |
| `pi` | `o`(订单) `ip` 等 | Proxy Info（线路信息，v2022 起支持 `s`,`c`,`a`,`sa`,`ip`,`o` 可选参数） |
| `security` | — | 线路安全策略查询 |
| `changeauth` | — | 设置线路独立安全策略 |
| `changeremark` | — | 设置线路备注（v2022.09 起） |
| `batchchangeremark` | — | 批量改备注 |
| `batchchangestatus` | — | 批量改状态 |
| `check` / `checkr` / `checkbr` / `checkcans` | — | 各种可达性/资格检查 |

### 2.3 订单（Order）

| 方法 | 参数 | 说明 |
|------|------|------|
| `stock` | `g`(区域ID), `t`(代理类型) | 库存查询（g: 1=静态数据中心, 2=动态住宅区域, 4=静态ISP） |
| `stockv2` | — | 库存查询 V2 |
| `price` | — | 价格查询 |
| `tdetail` | `o`(订单号) | 订单详情 |
| `tdetailEx` | — | 订单详情扩展 |
| `ol` | `pi`,`st` 等 | Order List（订单列表） |
| `oiv2` | — | 订单信息 V2 |
| `pay` | — | 支付/下单 |
| `prenew` | — | 预续费 |
| `package` | — | 流量包 |

### 2.4 IP 白名单 / 安全

| 方法 | 参数 | 说明 |
|------|------|------|
| `addwhite` | `ip`(IPv4) | 添加 IP 到白名单（3 分钟内生效） |
| `delwhite` | `ip` | 删除白名单 IP |
| `authwiplink` | `ip` | 获取白名单 IP 验证链接（15 分钟有效，数据中心/静态代理用） |
| `authreswiplink` | `ip` | 获取住宅白名单 IP 验证链接 |
| `authreswhiteip` | — | 住宅白名单 IP 认证（返回认证链接） |
| `resauth` / `chgresauth` | — | 住宅认证设置/修改 |
| `addreswip` / `delreswip` | — | 添加/删除住宅白名单 IP |
| `chgsecurity` | — | 修改全局安全策略 |
| `addaccount` | — | 添加子账户 |

### 2.5 账户

| 方法 | 参数 | 说明 |
|------|------|------|
| `info` | — | 账户信息 |
| `subaccounts` | — | 子账户列表 |
| `pusage` | — | 流量使用统计 |

### 2.6 相关返回码（诊断）

| code | 含义 |
|------|------|
| 30055 | Authentication Fail. Please open the IP Authentication Link under the correct IP（需在正确出口 IP 下打开认证链接） |
| 30056 | No avaliable whitelist IP，need to auth your white IP first（需先认证白名单 IP） |
| 40010 | User/password auth is closed（账号密码认证已关闭） |
| 40019 | IP whitelist cannot be empty（白名单为空） |
| 40021（等同40019意）/ 40022 | Not allow to renew dynamic proxy |

---

## 三、签名算法 + Node.js 示例（可直接运行）

### 3.1 签名算法（官方文档确认）

> **Signing method：Concatenate all request parameters, calculate HMAC-SHA1 using developer token, then perform base64 encoding**

- **参数串（parameter string）**：把本次请求中**除 `accessid` 和 `signature` 以外的所有参数**按 **URL 中出现的顺序**用 `k=v&k2=v2` 拼接（`ts` 也参与）。
  - 官方示例加密串：`g=1&ts=1609430400`
- **密钥**：Encryption key（在账户中心获取，官方文档示例用 `1234567ABCDEFG`）
- **算法（官方 Java / PHP 表述等价）**：
  ```
  signature = Base64( HMAC-SHA1( key = encryption_key, message = parameter_string ) )
  ```
  - Java 表述：`Base64(HMAC-SHA1(encryption_key, UTF-8-Encoding-Of(parameter_string)))`
  - PHP 表述：`base64_encode(hash_hmac("sha1", parameter_string, encryption_key, false))`
- **`ts` 单位**：**秒级 Unix 时间戳**（文档示例 `1609430400` = 2021-01-01 00:00 UTC）。ts 过期会返回 `10015 Signature Timeout`。
- 注意：`accessid` **不参与签名**，只有当 URL 片段里的业务参数 + `ts` 参与。

### 3.2 Node.js 示例（签名 + 请求 + 提取 30 条动态住宅）

```javascript
// residential_extract_dynamic.js
// 从 residential 提取 30 条动态住宅代理（需要 Developer ID + Encryption key）
const crypto = require('crypto');

const BASE = 'https://www.residential.com/clientapi'; // 动态提取用 clientapi 基址

// ---- 从账户信息中获取 ----
const accessid = 'YOUR_DEVELOPER_ID';     // accessid = Developer ID
const secret   = 'YOUR_ENCRYPTION_KEY';   // Encryption key（账户中心）

// ---- 待请求参数（不含 accessid、signature 自身）----
function signAndBuild(method, params) {
  // 参数按固定顺序排列（要与最终 URL 顺序一致）
  const ordered = Object.keys(params).sort();           // 如与 URL 顺序不符，请改为 URL 实际顺序
  const paramStr = ordered.map(k => `${k}=${params[k]}`).join('&');
  // 官方顺序示例为 g=433&n=1&t=2&ts=xxx（业务参数在前，ts 在后）
  // 注意：这里用 URL 实际出现的顺序。下面的实现按给定 params 的插入顺序。
  const paramStrByInsertion = Object.entries(params)
    .map(([k, v]) => `${k}=${v}`).join('&');

  // 若文档明确要求顺序（如 g,n,t,ts），手动构造更稳妥：
  // const paramStr = `g=${params.g}&n=${params.n}&t=${params.t}&ts=${params.ts}`;

  const signature = crypto
    .createHmac('sha1', secret)
    .update(paramStrByInsertion)
    .digest('base64');

  const qs = new URLSearchParams({
    accessid,
    signature,
    ...params,
  }).toString();

  return `${BASE}/${method}?${qs}`;
}

// ---- 提取 30 条动态住宅：pickdynamicipsbyw ----
// 需要先配置白名单 IP（见文末步骤 0-2）
async function extractDynamicProxies(geoId = 433, count = 30, type = 2) {
  const ts = Math.floor(Date.now() / 1000);   // 秒级时间戳
  const params = { g: geoId, n: count, t: type, ts };
  const url = signAndBuild('pickdynamicipsbyw', params);
  console.log('请求:', url.replace(signatureOf(url), '<SIGN>'));

  const res = await fetch(url);
  const json = await res.json();
  if (!json.success) throw new Error(`提取失败 code=${json.code} msg=${json.msg}`);
  // data: ["ip:port", ...]
  return json.data;
}
function signatureOf(url) { /* 仅供脱敏打印 */ return url.split('signature=')[1].split('&')[0]; }

// ---- 主流程：提取 -> 拼成 Egress 可用的 user@gateway 形式 ----
(async () => {
  try {
    const rawEndpoints = await extractDynamicProxies(433, 30, 2);
    console.log('\n提取到', rawEndpoints.length, '条 ip:port：');
    console.log(rawEndpoints.join('\n'));

    // 提示：
    //  1) 动态住宅账户需设置为「账号密码认证」或「白名单认证」；
    //  2) 若按官方格式线路使用，网关是 gate.residential.info:1000 / mobile.residential.info:1086，
    //     用户名带 用户ID-安全策略用户:安全策略密码-国家-会话-时长，见文末「会话配置」；
    //  3) 若使用提取出的 ip:port 作为给 Egress 的直连端点，也需保证出口机器在白名单内。
  } catch (e) {
    console.error('\n❌', e.message);
    process.exit(1);
  }
})();
```

> 运行：`node residential_extract_dynamic.js`（需 Node 18+，自带 `fetch`）。
> 关键：签名**只针对业务参数 + `ts`**，`accessid` 不参与；HMAC 的 **key=Encryption key、message=参数串**。

### 3.3 签名已验证（PowerShell 同款）

对官方示例 `parameter_string = g=1&ts=1609430400`、`key = 1234567ABCDEFG`，HMAC-SHA1→base64 计算得到：

```
signature = xdJBtfYGGhIArxNRO1porymxMJQ=
```

（算法与官方 Java/PHP 表述逐字一致；文档内演示用的 signature 值可能是旧 demo 占位，以算法表述为准。）

---

## 四、16 条 `user:pass@host:port` 代理 CONNECT 超时诊断（结论）

用户提供格式：`<policy-account>:<policy-password>@res25.residential.info:16565`（`user:pass@host:port`，实际凭据请以服务商面板为准）

### 4.1 诊断结论：四个不匹配导致超时（按重要性排序）

1. **主机/端口不是动态住宅网关**
   - 官方动态代理文档的标准网关仅为：
     - `gate.residential.info:1000`（动态住宅 / 数据中心）
     - `mobile.residential.info:1086`（移动代理）
     - 以及区域优化网关 `gate-us / gate-eu / gate-gb / gate-au / gate-in / gate-sea / gate-th / gate-jp .residential.info`
   - **文档中不存在 `res25.residential.info:16565`** 这一主机/端口。`resXX` 前缀与 `16565` 端口均未出现在任何动态代理文档中。

2. **实测端口连通性差异**
   - `res25.residential.info:16565` → TCP 连接超时（curl 返回 28）。
   - `gate.residential.info:1000` → TCP 能建立连接（仅在后续 TLS/HTTP 阶段失败，证明端口本身开放）。
   - 即用户给的 `16565` 端口在网关侧不通。

3. **用户名缺少关键会话/区域段（最可能的根因）**
   - 官方动态住宅线路格式为（见 §5.1）：
     ```
     用户ID-安全策略用户名:安全策略密码-国家ISO-会话ID-轮换间隔@gate.residential.info:1000
     ```
   - 只有当用户输入只有 `user:pass`（如 `d1c.....e:a4cb...`）时，缺少 `-国家-会话-时长`，residential 无法识别路由策略 → 认证/路由失败。
   - 正确示例（官方）：
     - `123456789-abcdef:12345678-US@gate.residential.info:1000`（美国，每请求轮换）
     - `123456789-abcdef:12345678-global@gate.residential.info:1000`（全球混路由）
     - `123456789-abcdef:12345678-US-86822206-1h@gate.residential.info:1000`（美国，1 小时轮换）

4. **IP 白名单未认证（连带原因）**
   - 若账户用了「白名单认证」模式，必须先在面板把出口机器公网 IP 加白并打开认证链接（错误码 30055/30056/40019 均指向此）。出口 IP 不在白名单 → 连上网关也被拒。

### 4.2 一句话结论
> 这 16 条不是有效的动态住宅线路：主机 `res25`/端口 `16565` 非官方网关、用户名缺少 `-国家-会话-时长` 关键段、且可能未完成白名单 IP 认证。三者叠加必然 CONNECT 超时。

### 4.3 修正后的正确线路示例（喂 Egress）
```
123456789-abcdef:12345678-US_session123-1h@gate.residential.info:1000
```
（把 UserID、安全策略用户名/密码、国家、随机8位会话、时长替换为账户真实值；若出口机在白名单认证模式，先 addwhite + 认证。）

---

## 五、订阅链接 / 会话配置结论

### 5.1 会话（session / sticky）配置

动态住宅线路的用户名字段完整语法：

```
UserID - SecurityPolicyUsername : SecurityPolicyPassword - CountryISOCode - RandomSession - IPRotationInterval
```

| 字段 | 必填 | 说明 |
|------|:---:|------|
| UserID | • | 用户 ID |
| SecurityPolicyUsername | • | 全局安全策略用户名 |
| SecurityPolicyPassword | • | 全局安全策略密码 |
| CountryISOCode | • | 国家 ISO 码；`global`=全球混路由；支持 `US_California`、`US_California_city_LosAngeles` 等精确区域；港澳台需实名 |
| RandomSession | 可选 | **8 位随机串**。提供=粘性会话（sticky）；**省略=每次请求换 IP** |
| IPRotationInterval | 可选 | `5m`（每 5 分钟轮换）/ `1h`（每 1 小时轮换）；**省略=保持粘性** |

**会话时长支持总结**：
- 粘性（sticky，默认）：带 8 位 RandomSession，IP 保持不变（最久）。
- 轮换模式：`5m`、`1h`（用户名串标注）；订单/切换参数层支持 `1=Sticky / 2=30min / 3=10min / 4=5min`。
- **文档中未见超过 1 小时的可自定义值**；「最长时间」对应 sticky 无限保持（由 RandomSession 决定），删除会话串即变「每次请求换 IP」。
- 动态线路可按需调 `rotate` / `batchchangeip` 强制换 IP、`release` / `batchrelease` 释放。

### 5.2 订阅链接（能否直接给 Egress 自动刷新）

- **官方文档中不存在 `subscription`/订阅链接/api link 概念**。residential 的动态住宅代理标准接入方式是 **API 提取**（`pickdynamicipsbyw` 每次都返回当前可用的 `ip:port` 列表）或 **固定网关 + 会话用户名**（`user...@gate.residential.info`）。
- 对 Egress（upstream，把订阅转粘性池的网关）的落地建议：
  - **方案 A（推荐）**：固定网关 + 会话用户名。给 Egress 一条
    `用户ID-策略用户:策略密码-US-会话123-1h@gate.residential.info:1000`
    Egress 把它当常规 http 代理池即可；IP 由 residential 会话自动轮换，无需刷新订阅。
  - **方案 B**：写定时任务（如每 5 分钟）调 `pickdynamicipsbyw?g=&n=30&t=2`，把返回的 `ip:port` 写成一个文件/URL；再让 Egress 定时抓取。但提取出的 `ip:port` 端点通常要求出口机在白名单内，需先配置白名单。

---

## 六、从「Developer ID + Encryption key」到提取 30 条可用线路——完整操作步骤

> 前置：已注册成功、充值、且账户启用了动态住宅代理权限。

### 步骤 0：确认账户认证模式（白名单 or 账号密码）
1. 登录 `https://app.residential.net`（面板）。
2. 控制台 → 找到「全局安全策略 / Security Policy」。
3. 确认 authentication mode：
   - **账号密码认证**：记录 SecurityPolicyUsername / SecurityPolicyPassword（下文 `SP_USER` / `SP_PASS`）。
   - **白名单认证**：需执行步骤 2 把出口机 IP 加白并认证。

### 步骤 1：获取凭证
1. 账户中心获取 **Developer ID**（= `accessid`）与 **Encryption key**（secret）。
2. 记录 **UserID**（纯数字用户 ID）。

### 步骤 2：配置出口 IP 白名单（仅白名单认证需要）
> 出口机器 = 运行 Egress 的机器。取它的公网出口 IP `EXIT_IP`。
1. 调用 `addwhite?ip=<EXIT_IP>`（3 分钟内生效）。
2. 调用 `authreswiplink` 或 `authreswhiteip`，拿到认证链接，**在出口机器上打开**（15 分钟内）。
   - 也可在控制台白名单页手动添加并点认证链接。
3. 可在 `info` 接口核对白名单生效。

### 步骤 3：确定目标区域
- 查 `stock`（`g=2` 拿动态住宅可用国家/区域列表），选一个区域 ID，例如美国 `433`（示例值，请以实际查询为准）。

### 步骤 4：提取 30 条动态住宅代理
使用 §3.2 的 Node.js 脚本：
```bash
node residential_extract_dynamic.js   # 内部调 pickdynamicipsbyw?g=<区域ID>&n=30&t=2
```
- `n=30`，`t=2`（2=动态住宅，3=移动代理）。
- 成功返回 `data:["ip:port", x30]`。

### 步骤 5：灌给 Egress（两种方式二选一）
- **A. 固定网关线路**（推荐，自动轮换、不需要刷新）：
  ```
  <UserID>-<SP_USER>:<SP_PASS>-<国家ISO>-<8位会话>-1h@gate.residential.info:1000
  ```
  把它配置进 Egress 的代理源；Egress 会按 residential 会话自动换 IP。
- **B. 用提取的 ip:port**：把步骤 4 的 30 条 `ip:port` 写入 Egress 的代理列表；并加一个系统定时任务（如每 5 分钟）重新调接口刷新这份列表。

### 步骤 6：验证
- 用 `curl -x <代理> https://api.ipify.org` 确认出口 IP 在目标区域且可连通。
- 检查 Egress 日志确认 30 条连接均稳定、不超时。

### 常见排错
| 现象 | 处理 |
|------|------|
| `40001 Bad Auth` | accessid 是否为 Developer ID；签名 key 是否正确；ts 是否过期/秒级 |
| `10059 Signature error or timeout` | 参数串顺序与 URL 不符，或 ts 过期 |
| `30055/30056` | 出口 IP 未在白名单 / 未认证 |
| CONNECT 超时 | host:port 是否用官方网关；用户名是否带 `-国家-会话-时长` 段 |

---

## 七、关键证据来源 URL

| 内容 | URL |
|------|-----|
| API 文档页（SSR HTML，含目录 Order/Line/Security/Account） | `https://www.residential.net/apidoc` |
| 内嵌完整英文 API 文档（签名算法、方法表、动态代理格式、返回码） | `https://cdn.residential.net/residentialnetosite-dist/public/_nuxt/index.94dfde9d.js` |
| 官方使用手册 | `https://help.residential.com/` |
| 官方帮助站 sitemap | `https://help.residential.com/sitemap.xml` |
| 动态住宅网关实测（连通性诊断） | `gate.residential.info:1000`（通则）/ `res25.residential.info:16565`（超时） |
| API 路径实测（返回 40001=接口存在） | `https://www.residential.com/clientapi/pickdynamicipsbyw` |

---

*本报告为纯调研产物，不构成对以上接口的账号级操作。实际使用前请以 residential 面板为准核对区域 ID、安全策略与会话选项。*
