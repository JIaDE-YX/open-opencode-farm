// 模拟 Client 的流式请求（OpenAI SDK stream:true 行为）
const fs = require('fs');
const { clientCredentials } = require('../_lib/paths');
const creds = fs.readFileSync(clientCredentials, 'utf8');
const KEY = creds.split('\n').map(l => l.trim()).find(l => l.startsWith('OPENCODE_LOCAL_API_KEY'))
  ?.split(':').slice(1).join(':').trim().replace(/^"|"$/g, '');

async function testStream(label, useStream) {
  const t0 = Date.now();
  try {
    const res = await fetch('http://localhost:8080/v1/chat/completions', {
      method: 'POST',
      headers: { Authorization: `Bearer ${KEY}`, 'Content-Type': 'application/json', Accept: 'text/event-stream' },
      body: JSON.stringify({
        model: 'deepseek-v4-flash-free',
        messages: [{ role: 'user', content: 'hi' }],
        max_tokens: 50,
        stream: useStream,
      }),
      signal: AbortSignal.timeout(60000),
    });
    if (!res.ok) { console.log(`${label}: HTTP ${res.status} ${(await res.text()).slice(0, 150)}`); return; }
    if (!useStream) {
      const j = await res.json();
      console.log(`${label}: ✅ 非流式 ${j.usage?.total_tokens} tokens ${Date.now() - t0}ms`);
      return;
    }
    // 流式：读 SSE 块
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let chunks = 0, data = '', firstChunkMs = 0;
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      if (!firstChunkMs) firstChunkMs = Date.now() - t0;
      chunks++;
      data += decoder.decode(value, { stream: true });
      if (chunks > 200) { console.log(`${label}: 块太多，中止`); reader.cancel(); break; }
    }
    const lines = data.split('\n').filter(l => l.startsWith('data:')).length;
    console.log(`${label}: ✅ 流式 ${chunks} 块 / ${lines} 个 data 行 / 首块 ${firstChunkMs}ms / 总 ${Date.now() - t0}ms`);
    console.log(`   尾部: ${data.slice(-200).replace(/\n/g, ' ')}`);
  } catch (e) {
    console.log(`${label}: ❌ ${e.cause?.code || e.message} ${Date.now() - t0}ms`);
    if (e.cause) console.log(`   cause: ${e.cause.code} ${e.cause.message}`);
  }
}

(async () => {
  await testStream('[非流式]', false);
  await testStream('[流式 SSE]', true);
})();
