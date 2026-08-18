// 对比测试：机场账户 vs residential 账户访问 opencode 上游的限额状态
const fs = require('fs');
const { execSync } = require('child_process');
const { gatewayConfig, proxyTokens } = require('../_lib/paths');
const cfg = JSON.parse(fs.readFileSync(gatewayConfig, 'utf8'));
const px = fs.readFileSync(proxyTokens, 'utf8').split('\n').map(l => l.trim()).find(l => l.startsWith('RESIN_PROXY_TOKEN='))?.split('=').slice(1).join('=');
// opencode 上游 key（bridge 用的）
const zenKey = cfg.zen_keys[0];
const goKey = cfg.go_keys[0];

function testVia(proxy, label) {
  try {
    const out = execSync(
      `curl -s --max-time 20 -x "http://${proxy}" -o /dev/null -w "%{http_code}" "https://opencode.ai/zen/v1/models" -H "Authorization: Bearer ${zenKey}"`,
      { encoding: 'utf8', timeout: 25000, stdio: ['ignore', 'pipe', 'ignore'] }
    );
    console.log(`${label}: HTTP ${out}`);
  } catch (e) {
    console.log(`${label}: 失败 ${(e.message || '').split('\n')[0].slice(0, 80)}`);
  }
}

console.log('=== opencode.ai/zen/v1/models 经各出口 ===');
testVia(`Default.user_1:${px}@127.0.0.1:2260`, '[机场 user_1]');
testVia(`residential.user_1:${px}@127.0.0.1:2260`, '[residential user_1]');
testVia(`residential.user_2:${px}@127.0.0.1:2260`, '[residential user_2]');
testVia(`residential.user_3:${px}@127.0.0.1:2260`, '[residential user_3]');
testVia(`residential.user_4:${px}@127.0.0.1:2260`, '[residential user_4]');
