// 导入住宅线路到出口代理池并测试连通性
const fs = require('fs');
const { proxyTokens } = require('../_lib/paths');
const TOK = fs.readFileSync(proxyTokens, 'utf8')
  .split('\n').map(l => l.trim()).find(l => l.startsWith('RESIN_ADMIN_TOKEN='))
  ?.split('=').slice(1).join('=');

const BASE = 'http://127.0.0.1:2260';

(async () => {
  // 1) 读线路 → socks5:// URL 格式订阅内容
  const lines = fs.readFileSync(process.env.OPENCODE_FARM_PROXY_LINES_FILE || 'proxies.txt', 'utf8')
    .split('\n').map(l => l.trim()).filter(Boolean);
  const content = lines.map(l => {
    const m = l.match(/^([^:@]+):([^@]+)@([^:]+):(\d+)$/);
    return `socks5://${m[1]}:${m[2]}@${m[3]}:${m[4]}`;
  }).join('\n');
  console.log('[1] 订阅内容（16 行 socks5://）已生成');

  // 2) 检查是否已有住宅订阅
  const subs = await (await fetch(BASE + '/api/v1/subscriptions', { headers: { Authorization: `Bearer ${TOK}` } })).json();
  const existing = (subs.items || []).find(s => s.name === 'residential');
  if (existing) {
    console.log('[2] residential 订阅已存在 id=' + existing.id + '，更新内容');
  }

  // 3) 创建/更新订阅
  const body = JSON.stringify({ name: 'residential', source_type: 'local', content, enabled: true });
  let res;
  if (existing) {
    res = await fetch(BASE + `/api/v1/subscriptions/${existing.id}`, { method: 'PATCH', headers: { Authorization: `Bearer ${TOK}`, 'Content-Type': 'application/json' }, body });
  } else {
    res = await fetch(BASE + '/api/v1/subscriptions', { method: 'POST', headers: { Authorization: `Bearer ${TOK}`, 'Content-Type': 'application/json' }, body });
  }
  const txt = await res.text();
  console.log('[3] 导入:', res.status, txt.slice(0, 200));

  // 4) 等 Egress 解析 + 探测
  console.log('[4] 等待 Egress 解析订阅与探测节点...');
  await new Promise(r => setTimeout(r, 8000));
  const subs2 = await (await fetch(BASE + '/api/v1/subscriptions', { headers: { Authorization: `Bearer ${TOK}` } })).json();
  for (const s of subs2.items || []) {
    console.log(`    订阅: ${s.name} | enabled=${s.enabled} | 内容=${s.content.length} 字节`);
  }

  // 5) 通过出口池账户测试住宅出口
  //    用 admin token 直接测：GET /api/v1/... 看有没有节点池状态
  for (const p of ['/api/v1/node-pool', '/api/v1/accounts', '/api/v1/status', '/api/v1/metrics/snapshots/node-pool']) {
    try {
      const r = await fetch(BASE + p, { headers: { Authorization: `Bearer ${TOK}` } });
      const t = await r.text();
      console.log(`    ${p} -> ${r.status} ${t.slice(0, 150)}`);
    } catch (e) { console.log(`    ${p} -> ${e.message}`); }
  }
})();
