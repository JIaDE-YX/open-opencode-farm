// 验证新 UI：登录 → 主面板 HTML 特征 + API 数据
const http = require('http');
const fs = require('fs');
const { gatewayConfig } = require('../_lib/paths');
const PASSWORD = JSON.parse(fs.readFileSync(gatewayConfig, 'utf8')).admin_password;
let cookie = '';

function req(method, path, body, headers = {}) {
  return new Promise((res) => {
    const r = http.request({
      host: '127.0.0.1', port: 8080, method, path,
      headers: { ...(cookie ? { Cookie: cookie } : {}), ...(body ? { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(body) } : {}), ...headers },
    }, (s) => {
      let d = ''; s.on('data', c => d += c);
      s.on('end', () => {
        const sc = s.headers['set-cookie'];
        if (sc) cookie = sc[0].split(';')[0];
        res({ status: s.statusCode, body: d });
      });
    });
    r.on('error', e => res({ status: 0, body: e.message }));
    if (body) r.write(body);
    r.end();
  });
}

(async () => {
  // 登录
  const q = new URLSearchParams({ password: PASSWORD }).toString();
  const login = await req('POST', '/admin/login', q, { 'Content-Type': 'application/x-www-form-urlencoded' });
  console.log('[登录]', login.status, 'cookie:', cookie ? '已设置' : '未设置');

  // 主面板
  const page = await req('GET', '/admin');
  const checks = [
    ['Tab 导航', page.body.includes('代理与 Key')],
    ['指标卡 m-uptime', page.body.includes('m-uptime')],
    ['Toast', page.body.includes('toasts')],
    ['新设计 token', page.body.includes('--accent:#6e7cf3')],
    ['模型搜索', page.body.includes('model-search')],
  ];
  checks.forEach(c => console.log(`[${c[0]}]`, c[1] ? '✅' : '❌'));
  console.log('[面板长度]', page.body.length, '字节');

  // API 冒烟
  for (const p of ['/api/admin/status', '/api/admin/proxies', '/api/admin/nodes', '/api/admin/models']) {
    const r = await req('GET', p);
    console.log(`[API ${p}]`, r.status, r.body.slice(0, 60));
  }
})();
