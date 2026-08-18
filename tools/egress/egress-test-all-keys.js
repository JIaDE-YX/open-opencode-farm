const { execSync } = require('child_process');
const fs = require('fs');

const { gatewayConfig, proxyTokens } = require('../_lib/paths');
const cfg = JSON.parse(fs.readFileSync(gatewayConfig, 'utf8'));
const px = fs.readFileSync(proxyTokens, 'utf8').split('\n').map(l => l.trim()).find(l => l.startsWith('RESIN_PROXY_TOKEN='))?.split('=').slice(1).join('=');

cfg.zen_keys.forEach((key, idx) => {
  const proxyUser = idx < 3 ? `Default.user_${idx+1}` : `residential.user_${idx-2}`;
  const proxyUrl = `${proxyUser}:${px}@127.0.0.1:2260`;
  const postData = JSON.stringify({
    model: 'deepseek-v4-flash',
    messages: [{ role: 'user', content: '回复OK' }],
    max_tokens: 10
  });

  console.log(`\n=== 测试 Key ${idx+1} (${key.slice(0, 12)}...) 经代理 [${proxyUser}] ===`);
  try {
    fs.writeFileSync('/tmp/opencode-farm-payload.json', postData);
    const cmd = `curl -s --max-time 15 -x "http://${proxyUrl}" -X POST "https://opencode.ai/zen/go/v1/chat/completions" -H "Authorization: Bearer ${key}" -H "Content-Type: application/json" -H "User-Agent: opencode/0.1.0" -d "@/tmp/opencode-farm-payload.json"`;
    const res = execSync(cmd, { encoding: 'utf8', timeout: 20000 });
    console.log('响应:', res.slice(0, 250));
  } catch (e) {
    console.log('失败:', (e.message || '').split('\n')[0]);
  }
});
