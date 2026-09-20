'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('playwright');

const ROOT = path.resolve(__dirname, '..');
const SCREENSHOTS = path.join(ROOT, 'dist', 'RuStore', 'screenshots');
const REPORT = path.join(ROOT, 'docs', 'YARUS-1.0.0-stock-report-example.pdf');
const EDGE = 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe';

const ADAPTER = `
const mem=new Map();
openDB=async()=>({});getKV=async k=>mem.has(k)?D.copy(mem.get(k)):undefined;
putKV=async(k,v)=>{mem.set(k,D.copy(v));};delKV=async k=>{mem.delete(k);};
applyLocal=async c=>{const result=D.execute(await getKV(activeKey())||state,c);await putKV(activeKey(),result);return result;};
mutateQueue=async edit=>{queue=edit(await getKV(queueKey())||[]);await putKV(queueKey(),queue);return queue;};
saveCfg=()=>{};inferServer=()=>'';
init();
`;

function source(name) {
  return fs.readFileSync(path.join(ROOT, 'web', name), 'utf8');
}

async function mount(browser, viewport, deviceScaleFactor) {
  const context = await browser.newContext({ viewport, deviceScaleFactor, locale: 'ru-RU', acceptDownloads: true });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(String(error)));
  let html = source('index.html')
    .replace('<link rel="stylesheet" href="style.css">', `<style>${source('style.css')}</style>`)
    .replace(/<script src="[^"]+"><\/script>/g, '')
    .replace(/<meta http-equiv="Content-Security-Policy"[^>]+>/g, '');
  await page.setContent(html);
  for (const name of ['domain.js', 'vendor/qr-encode.js', 'vendor/qr-decode.js', 'vendor/code128-patterns.js', 'codes.js', 'reports.js', 'scanner.js']) {
    await page.addScriptTag({ content: source(name) });
  }
  let app = source('app.js').trimEnd();
  assert.ok(app.endsWith('init();'));
  app = app.slice(0, -7) + ADAPTER;
  await page.addScriptTag({ content: app });
  await page.getByRole('button', { name: /Сначала посмотреть на примере/ }).click();
  await page.waitForSelector('#main');
  await page.evaluate(() => document.fonts.ready);
  assert.equal(await page.evaluate(() => activeItems().length), 12);
  return { context, page, errors };
}

async function shot(page, folder, name) {
  await page.evaluate(() => { window.scrollTo(0, 0); document.querySelector('#toasts').innerHTML = ''; });
  await page.screenshot({ path: path.join(folder, name), fullPage: false, animations: 'disabled' });
}

async function phoneShots(browser) {
  const folder = path.join(SCREENSHOTS, 'phone');
  fs.mkdirSync(folder, { recursive: true });
  const { context, page, errors } = await mount(browser, { width: 360, height: 640 }, 3);
  await shot(page, folder, '01-overview-1080x1920.png');
  await page.locator('.mobile-nav [data-nav="stock"]').click();
  await page.evaluate(() => {
    URL.createObjectURL = () => { throw new Error('legacy object URL path must not be used'); };
    HTMLImageElement.prototype.decode = () => Promise.reject(new Error('legacy image.decode path must not be used'));
  });
  await page.locator('[data-action="scan-code"]').first().click();
  await page.locator('#scan-image').setInputFiles(path.join(ROOT, 'tests', 'fixtures', 'bar-EAN13-460123456789.png'));
  await page.waitForFunction(() => document.querySelector('#scan-result')?.textContent.includes('4601234567893'));
  assert.match(await page.locator('#scan-status').textContent(), /Код прочитан.*EAN-13/);
  await page.locator('#scan-close').click();
  await shot(page, folder, '02-stock-1080x1920.png');
  await page.locator('.mobile-nav [data-nav="history"]').click();
  await shot(page, folder, '03-history-1080x1920.png');
  await page.locator('.mobile-nav [data-nav="stock"]').click();
  await page.locator('.mobile-stock-item').first().click();
  await page.locator('#modal-root [data-action="label-item"]').click();
  await page.waitForSelector('.label-preview canvas');
  await shot(page, folder, '04-qr-label-1080x1920.png');
  await page.locator('[data-action="close-modal"]').first().click();
  await page.locator('.mobile-nav [data-nav="settings"]').click();
  await shot(page, folder, '05-settings-1080x1920.png');
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  assert.deepEqual(errors, []);
  await context.close();
}

async function tabletShotsAndReport(browser) {
  const folder = path.join(SCREENSHOTS, 'tablet');
  fs.mkdirSync(folder, { recursive: true });
  const { context, page, errors } = await mount(browser, { width: 800, height: 1280 }, 2);
  await shot(page, folder, '01-overview-1600x2560.png');
  await page.locator('.nav [data-nav="stock"]').click();
  await shot(page, folder, '02-stock-1600x2560.png');
  await page.locator('.nav [data-nav="places"]').click();
  await shot(page, folder, '03-places-1600x2560.png');
  await page.locator('.nav [data-nav="history"]').click();
  await shot(page, folder, '04-history-1600x2560.png');
  await page.locator('.nav [data-nav="settings"]').click();
  await page.locator('[data-action="export-pdf"]').click();
  await shot(page, folder, '05-pdf-report-1600x2560.png');
  const downloadEvent = page.waitForEvent('download');
  await page.locator('button[form="report-form"]').click();
  const download = await downloadEvent;
  await download.saveAs(REPORT);
  assert.ok(fs.readFileSync(REPORT).subarray(0, 8).toString('ascii').startsWith('%PDF-1.4'));
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  assert.deepEqual(errors, []);
  await context.close();
}

(async () => {
  fs.mkdirSync(path.dirname(REPORT), { recursive: true });
  const browser = await chromium.launch({ executablePath: EDGE, headless: true, args: ['--disable-gpu'] });
  try {
    await phoneShots(browser);
    await tabletShotsAndReport(browser);
  } finally {
    await browser.close();
  }
  console.log('PASS: responsive phone/tablet UI, QR preview, PDF workflow and RuStore screenshots');
})().catch(error => { console.error(error); process.exitCode = 1; });
