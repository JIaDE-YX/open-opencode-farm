// 模拟 Client 调用 bridge：直连 vs 走代理
const fs = require('fs');
const { execSync } = require('child_process');
const { clientCredentials } = require('../_lib/paths');
const creds = fs.readFileSync(clientCredentials, 'utf8');
const KEY = creds.split('\n').map(l => l.trim()).find(l => l.startsWith('OPENCODE_LOCAL_API_KEY'))
  ?.split(':').slice(1).join(':').trim().replace(/^"|"$/g, '');
console.log('key:', KEY.slice(0, 12) + '...');

const BODY = JSON.stringify({ model: 'deepseek-v4-flash-free', messages: [{ role: 'user', content: 'hi' }], max_tokens: 10 });

async function testNodeFetch(label) {
  const t0 = Date.now();
  try {
    const res = await fetch('http://localhost:8080/v1/chat/completions', {
      method: 'POST',
      headers: { Authorization: `Bearer ${KEY}`, 'Content-Type': 'application/json' },
      body: BODY,
      signal: AbortSignal.timeout(30000),
    });
    const txt = await res.text();
    console.log(`${label}: HTTP ${res.status} ${txt.slice(0, 100)} ${Date.now() - t0}ms`);
  } catch (e) {
    console.log(`${label}: ❌ ${e.cause?.code || e.message} ${Date.now() - t0}ms`);
  }
}

(async () => {
  await testNodeFetch('[node fetch] 直连');

  // curl 带/不带系统代理
  try {
    const out = execSync('curl -s --max-time 10 -o /dev/null -w "%{http_code}" http://localhost:8080/healthz', { encoding: 'utf8', timeout: 15000 });
    console.log('[curl 默认(系统代理)]:', out);
  } catch (e) { console.log('[curl 默认] 失败:', (e.message || '').split('\n')[0].slice(0, 80)); }
  try {
    const out = execSync('curl -s --max-time 10 --noproxy "*" -o /dev/null -w "%{http_code}" http://localhost:8080/healthz', { encoding: 'utf8', timeout: 15000 });
    console.log('[curl --noproxy]', out);
  } catch (e) { console.log('[curl --noproxy] 失败:', (e.message || '').split('\n')[0].slice(0, 80)); }
  // 显式走代理
  try {
    const out = execSync('curl -s --max-time 10 -x http://127.0.0.1:7890 -o /dev/null -w "%{http_code}" http://localhost:8080/healthz', { encoding: 'utf8', timeout: 15000 });
    console.log('[curl -x 7890]', out);
  } catch (e) { console.log('[curl -x 7890] 失败:', (e.message || '').split('\n')[0].slice(0, 80)); }
})();
