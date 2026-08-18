// 刷新 residential 订阅并检查节点池
const fs = require('fs');
const { proxyTokens } = require('../_lib/paths');
const TOK = fs.readFileSync(proxyTokens, 'utf8')
  .split('\n').map(l => l.trim()).find(l => l.startsWith('RESIN_ADMIN_TOKEN='))
  ?.split('=').slice(1).join('=');

const BASE = 'http://127.0.0.1:2260';
const SUB_ID = '30deed78-fabc-4d63-8404-0df3b5f22596';

(async () => {
  const H = { Authorization: `Bearer ${TOK}`, 'Content-Type': 'application/json' };

  // 1) 刷新订阅
  const r = await fetch(`${BASE}/api/v1/subscriptions/${SUB_ID}/actions/refresh`, { method: 'POST', headers: H });
  console.log('[1] refresh:', r.status, (await r.text()).slice(0, 200));

  // 2) 等解析
  await new Promise(res => setTimeout(res, 6000));

  // 3) 节点池
  const pool = await (await fetch(BASE + '/api/v1/metrics/snapshots/node-pool', { headers: H })).json();
  console.log('[2] node-pool:', JSON.stringify(pool).slice(0, 300));

  // 4) 节点列表（找 residential 节点）
  const nodes = await (await fetch(BASE + '/api/v1/nodes', { headers: H })).json();
  const items = nodes.items || nodes || [];
  console.log('[3] 节点总数:', Array.isArray(items) ? items.length : JSON.stringify(nodes).slice(0, 200));
  if (Array.isArray(items)) {
    const kk = items.filter(n => JSON.stringify(n).includes('residential'));
    console.log('    residential 节点:', kk.length);
    kk.slice(0, 3).forEach(n => console.log('    -', JSON.stringify(n).slice(0, 200)));
  }
})();
