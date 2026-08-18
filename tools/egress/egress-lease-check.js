// 查看 Egress 平台账户租赁与节点分配
const fs = require('fs');
const { proxyTokens } = require('../_lib/paths');
const TOK = fs.readFileSync(proxyTokens, 'utf8')
  .split('\n').map(l => l.trim()).find(l => l.startsWith('RESIN_ADMIN_TOKEN='))
  ?.split('=').slice(1).join('=');

const BASE = 'http://127.0.0.1:2260';
const H = { Authorization: `Bearer ${TOK}` };
const PLATFORM = '00000000-0000-0000-0000-000000000000';

(async () => {
  // 1) 平台详情（完整）
  const p = await (await fetch(`${BASE}/api/v1/platforms/${PLATFORM}`, { headers: H })).json();
  console.log('[1] 平台:', JSON.stringify(p).slice(0, 800));

  // 2) 租赁（账户→节点）
  const leases = await (await fetch(`${BASE}/api/v1/platforms/${PLATFORM}/leases`, { headers: H })).json();
  console.log('\n[2] 租赁:', JSON.stringify(leases).slice(0, 1200));

  // 3) 节点列表完整（找 egress IP）
  const nodes = await (await fetch(BASE + '/api/v1/nodes', { headers: H })).json();
  const items = nodes.items || [];
  console.log('\n[3] 节点数:', items.length);
  // 显示每个节点的 display_tag + 是否 residential
  const kk = items.filter(n => (n.display_tag || '').includes('residential'));
  const other = items.filter(n => !(n.display_tag || '').includes('residential'));
  console.log('residential:', kk.length, '| 其他:', other.length);
  // 取前几个节点完整信息看字段
  items.slice(0, 3).forEach(n => console.log('  节点样例:', JSON.stringify(n).slice(0, 300)));
})();
