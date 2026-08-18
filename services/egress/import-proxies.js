// 导入住宅/机场线路到出口代理池
// 用法: node import-proxies.js <线路文件> [订阅名] [--skip-test]
const fs = require('fs');
const { proxyTokens } = require('../../tools/_lib/paths');

const adminToken = fs.readFileSync(proxyTokens, 'utf8')
  .split('\n').map(l => l.trim()).find(l => l.startsWith('RESIN_ADMIN_TOKEN='))
  ?.split('=').slice(1).join('=');

const file = process.argv[2];
const name = process.argv[3] || 'residential-pool';
if (!file) { console.error('usage: node import-proxies.js <file> [name] [--skip-test]'); process.exit(1); }

function normalize(line) {
  line = line.trim();
  if (!line || line.startsWith('#')) return null;
  if (line.includes('://')) {
    const u = new URL(line);
    return `${u.username}:${decodeURIComponent(u.password)}@${u.hostname}:${u.port || 80}`;
  }
  const m = line.match(/^([^:@]+):([^@]+)@([^:]+):(\d+)$/);
  if (m) return `${m[1]}:${m[2]}@${m[3]}:${m[4]}`;
  const m2 = line.match(/^([^:]+):(\d+):([^:]+):(.+)$/);
  if (m2) return `${m2[3]}:${m2[4]}@${m2[1]}:${m2[2]}`;
  const m3 = line.match(/^([^:]+):(\d+)$/);
  if (m3) return `${m3[1]}:${m3[2]}`;
  return null;
}

(async () => {
  const raw = fs.readFileSync(file, 'utf8');
  const lines = raw.split('\n').map(normalize).filter(Boolean);
  console.log('parsed lines:', lines.length);

  const skipTest = process.argv.includes('--skip-test');
  const alive = [];
  if (!skipTest) {
    const { execSync } = require('child_process');
    for (const l of lines) {
      try {
        const out = execSync(
          `curl -s -x "http://${l}" --max-time 8 "https://api.ipify.org?format=json"`,
          { encoding: 'utf8', timeout: 10000, stdio: ['ignore', 'pipe', 'ignore'] }
        );
        const ip = JSON.parse(out).ip;
        alive.push({ line: l, ip });
      } catch { /* dead */ }
    }
    console.log('alive:', alive.length, '/', lines.length);
  } else {
    lines.forEach(l => alive.push({ line: l, ip: '?' }));
  }

  const content = alive.map(a => a.line).join('\n');
  const body = JSON.stringify({ name, source_type: 'local', content, enabled: true });
  const res = await fetch('http://127.0.0.1:2260/api/v1/subscriptions', {
    method: 'POST',
    headers: { 'Authorization': `Bearer ${adminToken}`, 'Content-Type': 'application/json' },
    body,
  });
  const text = await res.text();
  console.log('egress import:', res.status, text.slice(0, 300));

  const ips = new Set(alive.map(a => a.ip).filter(i => i !== '?'));
  console.log('unique IPs:', ips.size);
})();
