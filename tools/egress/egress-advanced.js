// 探索 Egress 高级功能：系统配置、端点、账户头规则、平台能力
const fs = require('fs');
const { proxyTokens } = require('../_lib/paths');
const TOK = fs.readFileSync(proxyTokens, 'utf8')
  .split('\n').map(l => l.trim()).find(l => l.startsWith('RESIN_ADMIN_TOKEN='))
  ?.split('=').slice(1).join('=');

const BASE = 'http://127.0.0.1:2260';
const H = { Authorization: `Bearer ${TOK}` };

(async () => {
  // 1) 系统配置（全部环境变量生效值）
  const cfg = await (await fetch(BASE + '/api/v1/system/config', { headers: H })).json();
  console.log('[1] system/config:', JSON.stringify(cfg, null, 1).slice(0, 1500));

  // 2) 端点（自定义端口）
  const eps = await (await fetch(BASE + '/api/v1/endpoints', { headers: H })).json();
  console.log('\n[2] endpoints:', JSON.stringify(eps).slice(0, 600));

  // 3) 账户头规则
  const rules = await (await fetch(BASE + '/api/v1/account-header-rules', { headers: H })).json();
  console.log('\n[3] account-header-rules:', JSON.stringify(rules).slice(0, 600));

  // 4) 平台详情（residential 平台的更多字段）
  const plats = await (await fetch(BASE + '/api/v1/platforms', { headers: H })).json();
  for (const p of plats.items || []) {
    console.log(`\n[4] 平台 ${p.name}: sticky_ttl=${p.sticky_ttl} allocation=${p.allocation_policy} routable=${p.routable_node_count} regex=${JSON.stringify(p.regex_filters)} region=${JSON.stringify(p.region_filters)}`);
  }

  // 5) GeoIP 状态
  const geo = await (await fetch(BASE + '/api/v1/geoip/status', { headers: H })).json();
  console.log('\n[5] geoip:', JSON.stringify(geo).slice(0, 300));
})();
