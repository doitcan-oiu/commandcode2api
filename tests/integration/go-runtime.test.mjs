import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as sleep } from 'node:timers/promises';
import { setup } from './helpers.mjs';

const CHAT = { model: 'm', messages: [{ role: 'user', content: 'hi' }] };

test('streaming header wait uses its own idle limit instead of the non-streaming limit', async () => {
  const s = await setup({
    env: { CC_STREAM_IDLE_MS: '1000', CC_NONSTREAM_IDLE_MS: '50' },
    onRequest: async req => {
      if (req.url === '/alpha/generate') await sleep(200);
    },
  });
  try {
    const response = await s.proxy.post('/v1/chat/completions', { ...CHAT, stream: true }, {
      Authorization: 'Bearer user_header_timeout',
    });
    const body = await response.text();
    assert.equal(response.status, 200, body);
    assert.ok(body.includes('hello'));
    assert.ok(body.includes('[DONE]'));
  } finally {
    await s.close();
  }
});

test('Go runtime preserves the fixed CLI protocol version on generation and lifecycle requests', async () => {
  const s = await setup();
  try {
    const response = await s.proxy.post('/v1/chat/completions', CHAT, { Authorization: 'Bearer user_protocol' });
    assert.equal(response.status, 200);
    await response.json();
    const generate = s.mock.lastGenerate();
    assert.equal(generate.headers['x-command-code-version'], '1.53.1');
    const lifecycle = s.mock.seen.find(request => request.url === '/alpha/lifecycle-events');
    assert.ok(lifecycle, 'the CLI lifecycle request must still be sent');
    assert.equal(lifecycle.headers['x-command-code-version'], '1.53.1');
    assert.equal(JSON.parse(lifecycle.raw).metadata.cliVersion, '1.53.1');
    assert.ok(s.mock.seen.some(request => request.url === '/alpha/fingerprint/record'));
  } finally {
    await s.close();
  }
});

test('standalone healthcheck honors the configured host and port', async () => {
  const s = await setup();
  try {
    const check = spawnSync(s.proxy.child.spawnfile, ['-healthcheck'], {
      env: { ...process.env, PORT: String(s.proxy.port), HOST: '127.0.0.1', CC_CHECK_PROTOCOL_DRIFT: 'false' },
      timeout: 5000,
      encoding: 'utf8',
    });
    assert.equal(check.status, 0, check.error?.message || check.stderr || check.stdout);
    assert.equal(s.mock.seen.length, 0, 'health checks must not contact the upstream');
  } finally {
    await s.close();
  }
});

test('legacy config.json apiKey never replaces per-request authentication', async () => {
  const cwd = mkdtempSync(join(tmpdir(), 'ccp-config-test-'));
  mkdirSync(join(cwd, 'configs'));
  writeFileSync(join(cwd, 'configs', 'config.json'), JSON.stringify({ apiKey: 'user_config' }));
  let s;
  try {
    s = await setup({ cwd });
    const missing = await s.proxy.post('/v1/chat/completions', CHAT);
    assert.equal(missing.status, 401);
    await missing.json();
    assert.equal(s.mock.generateCount(), 0);
    const explicit = await s.proxy.post('/v1/chat/completions', CHAT, { Authorization: 'Bearer user_explicit' });
    assert.equal(explicit.status, 200);
    await explicit.json();
    assert.equal(s.mock.lastGenerate().headers.authorization, 'Bearer user_explicit');
  } finally {
    if (s) await s.close();
    rmSync(cwd, { recursive: true, force: true });
  }
});
