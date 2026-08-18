// 创建 residential 独立平台：regex_filters 匹配 residential 节点
const fs = require('fs');
const { proxyTokens } = require('../_lib/paths');
const TOK = fs.readFileSync(proxyTokens, 'utf8')
  .split('\n').map(l => l.trim()).find(l => l.startsWith('RESIN_ADMIN_TOKEN='))
  ?.split('=').slice(1).join('=');

const BASE = 'http://127.0.0.1:2260';
const H = { Authorization: `Bearer ${TOK}`, 'Content-Type': 'application/json' };

(async () => {
  // 1) 看现有平台详情（确认 regex_filters 格式）
  const p = await (await fetch(BASE + '/api/v1/platforms', { headers: H })).json();
  console.log('[1] 现有平台:', JSON.stringify(p).slice(0, 400));

  // 2) 试创建 residential 平台
  const body = {
    name: 'residential',
    regex_filters: ['residential'],
    allocation_policy: 'BALANCED',
  };
  const r = await fetch(BASE + '/api/v1/platforms', { method: 'POST', headers: H, body: JSON.stringify(body) });
  const t = await r.text();
  console.log('[2] 创建平台:', r.status, t.slice(0, 400));
})();
