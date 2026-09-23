'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('playwright');

const ROOT = path.resolve(__dirname, '..');
const EDGE = 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe';
const source = name => fs.readFileSync(path.join(ROOT, 'web', name), 'utf8');
const ADAPTER = `
const mem=new Map();
openDB=async()=>({});getKV=async k=>mem.has(k)?D.copy(mem.get(k)):undefined;
putKV=async(k,v)=>{mem.set(k,D.copy(v));};delKV=async k=>{mem.delete(k);};
applyLocal=async c=>{const result=D.execute(await getKV(activeKey())||state,c);await putKV(activeKey(),result);return result;};
mutateQueue=async edit=>{queue=edit(await getKV(queueKey())||[]);await putKV(queueKey(),queue);return queue;};
saveCfg=()=>{};inferServer=()=>'';recordAppDay=()=>{};recordMeaningfulUse=()=>{};
init();
`;

(async () => {
  const browser = await chromium.launch({ executablePath: EDGE, headless: true, args: ['--disable-gpu'] });
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, locale: 'ru-RU' });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(String(error)));
  try {
    let html = source('index.html')
      .replace('<link rel="stylesheet" href="style.css">', `<style>${source('style.css')}</style>`)
      .replace(/<script src="[^"]+"><\/script>/g, '')
      .replace(/<meta http-equiv="Content-Security-Policy"[^>]+>/g, '');
    await page.setContent(html);
    for (const name of ['domain.js', 'transport.js', 'host.js', 'transfer.js', 'vendor/qr-encode.js', 'vendor/qr-decode.js', 'vendor/code128-patterns.js', 'codes.js', 'reports.js', 'scanner.js']) {
      await page.addScriptTag({ content: source(name) });
    }
    await page.evaluate(() => {
      window.__portable = { attached: false, name: '', createdText: '', lastWrite: '', writes: 0 };
      window.AndroidFiles = {
        portableInfo() { return JSON.stringify({ attached: window.__portable.attached, name: window.__portable.name, bytes: window.__portable.lastWrite.length }); },
        createPortable(name, text) {
          Object.assign(window.__portable, { attached: true, name, createdText: text, lastWrite: text });
          setTimeout(() => window.YarusPortable.created(this.portableInfo(), ''), 0);
        },
        openPortable() {},
        writePortable(text) { window.__portable.lastWrite = text; window.__portable.writes++; return ''; },
        confirmPortable() { window.__portable.attached = true; return this.portableInfo(); },
        cancelPortable() {},
        detachPortable() { window.__portable.attached = false; },
        updateWidget() {}, requestReview() {}, closeApp() {}
      };
    });
    let app = source('app.js').trimEnd();
    assert.ok(app.endsWith('init();'));
    app = app.slice(0, -7) + ADAPTER;
    await page.addScriptTag({ content: app });

    await page.waitForSelector('[data-action="portable-open"]');
    assert.equal(await page.getByRole('button', { name: /Открыть файл личного склада/ }).isVisible(), true);
    await page.getByRole('button', { name: /Мой склад на этом устройстве/ }).click();
    await page.locator('#local-form [name="name"]').fill('Переносимый склад');
    await page.locator('button[form="local-form"]').click();
    await page.waitForSelector('#main');
    await page.locator('.mobile-nav [data-nav="settings"]').click();
    await page.getByRole('button', { name: /Создать и подключить файл/ }).click();
    await page.waitForFunction(() => window.__portable.attached);

    const created = await page.evaluate(() => ({ name: __portable.name, value: JSON.parse(__portable.createdText) }));
    assert.match(created.name, /^YARUS-.*\.yarus\.json$/);
    assert.equal(created.value.format, 'yarus-data');
    assert.equal(created.value.version, 2);
    assert.equal(created.value.portable.appVersion, '1.2.0');
    assert.equal(created.value.me, undefined);
    assert.equal(created.value.localSeen, undefined);

    await page.evaluate(async () => {
      await send({
        id: 'portable-command-0001', type: 'item',
        item: { id: 'portable-item-0001', name: 'Канистра', sku: 'К-001', barcode: '', category: 'Тара', unit: 'шт', min: 0, price: 0, fields: {}, note: '', archived: false, version: 0 }
      });
    });
    const mirrored = await page.evaluate(() => ({ writes: __portable.writes, value: JSON.parse(__portable.lastWrite) }));
    assert.ok(mirrored.writes >= 1);
    assert.equal(mirrored.value.items['portable-item-0001'].name, 'Канистра');

    await page.evaluate(async () => {
      const recovery = __portable.lastWrite;
      state = null;
      await delKV('local');
      __portable.attached = false;
      await YarusPortable.opened(recovery, 'Recovered.yarus.json');
    });
    assert.equal(await page.evaluate(() => state.items['portable-item-0001'].name), 'Канистра');
    assert.equal(await page.evaluate(() => __portable.attached), true);
    assert.deepEqual(errors, []);
    console.log('PASS: universal Android warehouse file create, auto-mirror and reinstall recovery flow');
  } finally {
    await context.close();
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
