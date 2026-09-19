'use strict';
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const net = require('node:net');
const path = require('node:path');
const { spawn } = require('node:child_process');

const ROOT = path.resolve(__dirname, '..');
const EXE = path.join(ROOT, 'dist', 'YARUS-server.exe');
const DATA = path.join(ROOT, 'tmp', `sync-live-${crypto.randomBytes(5).toString('hex')}`);
const id = () => crypto.randomBytes(16).toString('hex');

async function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const port = server.address().port;
      server.close(() => resolve(port));
    });
  });
}

async function api(base, route, body, token = '') {
  const response = await fetch(base + route, {
    method: body === undefined ? 'GET' : 'POST',
    headers: { ...(body === undefined ? {} : { 'content-type': 'application/json' }), ...(token ? { authorization: `Bearer ${token}` } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const result = await response.json();
  if (!response.ok) throw Object.assign(new Error(result.error || `HTTP ${response.status}`), { status: response.status, result });
  return result;
}

function launch(port) {
  const child = spawn(EXE, ['--headless', '--listen', `127.0.0.1:${port}`, '--data', DATA], { cwd: ROOT, windowsHide: true });
  let output = '';
  child.stdout.on('data', chunk => { output += chunk.toString(); });
  child.stderr.on('data', chunk => { output += chunk.toString(); });
  child.on('error', error => { output += `\nSPAWN ERROR: ${error.message}`; });
  child.on('exit', code => { output += `\nPROCESS EXIT: ${code}`; });
  return { child, output: () => output };
}

async function waitUntil(check, timeout = 10000) {
  const started = Date.now();
  while (Date.now() - started < timeout) {
    const value = await check();
    if (value) return value;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  throw new Error('Timed out waiting for YARUS server');
}

async function stop(child) {
  if (child.exitCode !== null) return;
  child.kill();
  await Promise.race([
    new Promise(resolve => child.once('exit', resolve)),
    new Promise((_, reject) => setTimeout(() => reject(new Error('Server did not stop')), 5000)),
  ]);
}

function quantity(state, item, place = '') {
  return Object.values(state.stocks).filter(stock => stock.item === item && (!place || stock.place === place)).reduce((sum, stock) => sum + stock.qty, 0);
}

(async () => {
  assert.ok(fs.existsSync(EXE), 'Build Windows server first');
  fs.mkdirSync(DATA, { recursive: true });
  const port = await freePort();
  const base = `http://127.0.0.1:${port}`;
  let process = launch(port);
  let ownerToken;
  try {
    let setup;
    try {
      setup = await waitUntil(async () => process.output().match(/setup=([^\s]+)/)?.[1]);
    } catch (error) {
      throw new Error(`${error.message}\n${process.output()}`);
    }
    const owner = await api(base, '/api/setup', { setup, login: 'owner', name: 'Владелец', password: 'sync-test-password', space: 'Склад синхронизации' });
    ownerToken = owner.token;
    const invitation = await api(base, '/api/invite', { role: 'editor' }, ownerToken);
    const employee = await api(base, '/api/join', { code: invitation.code, login: 'worker', name: 'Кладовщик', password: 'sync-test-password' });
    const workerToken = employee.token;

    await api(base, '/api/command', { id: id(), type: 'item', item: { id: 'sync-item', name: 'Тест синхронизации', sku: 'SYNC-001', barcode: '4601234567893', category: 'Тест', unit: 'шт', min: 1000, price: 5000, note: '', fields: {}, archived: false, version: 0 } }, ownerToken);
    await api(base, '/api/command', { id: id(), type: 'in', itemId: 'sync-item', to: 'main-place', qty: 5000 }, ownerToken);
    const workerState = await api(base, '/api/state', undefined, workerToken);
    assert.equal(quantity(workerState, 'sync-item'), 5000, 'second client did not receive stock');

    const movement = { id: id(), type: 'out', itemId: 'sync-item', from: 'main-place', qty: 2000 };
    const first = await api(base, '/api/command', movement, workerToken);
    const repeated = await api(base, '/api/command', movement, workerToken);
    assert.equal(repeated.duplicate, true, 'server did not identify retry');
    assert.equal(quantity(first.state, 'sync-item'), 3000);
    assert.equal(quantity(repeated.state, 'sync-item'), 3000, 'retry duplicated stock movement');
    await stop(process.child);

    const secondPort = await freePort();
    const secondBase = `http://127.0.0.1:${secondPort}`;
    process = launch(secondPort);
    await waitUntil(async () => { try { return (await api(secondBase, '/api/info')).ready; } catch { return false; } });
    const restored = await api(secondBase, '/api/state', undefined, ownerToken);
    assert.equal(quantity(restored, 'sync-item'), 3000, 'restart lost durable stock');
    assert.ok(fs.statSync(path.join(DATA, 'yarus.journal')).size > 0);
    console.log('PASS: two authenticated clients share one stock');
    console.log('PASS: retry after a lost acknowledgement is applied exactly once');
    console.log('PASS: journal and sessions survive a real server restart');
  } finally {
    await stop(process.child).catch(() => {});
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
