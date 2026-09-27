'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('playwright');

const ROOT = path.resolve(__dirname, '..');
const SCREENSHOTS = path.join(ROOT, 'dist', 'RuStore', 'screenshots');
const REPORT = path.join(ROOT, 'docs', 'YARUS-1.3.1-stock-report-example.pdf');
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
  for (const name of ['domain.js', 'transport.js', 'host.js', 'transfer.js', 'vendor/qr-encode.js', 'vendor/qr-decode.js', 'vendor/code128-patterns.js', 'codes.js', 'reports.js', 'scanner.js']) {
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

async function shot(page, folder, name, selector = '') {
  await page.evaluate(value => {
    if (value) {
      const element = document.querySelector(value);
      if (element) window.scrollTo(0, Math.max(0, window.scrollY + element.getBoundingClientRect().top - 180));
    } else window.scrollTo(0, 0);
    document.querySelector('#toasts').innerHTML = '';
  }, selector);
  await page.screenshot({ path: path.join(folder, name), fullPage: false, animations: 'disabled' });
}

async function phoneShots(browser) {
  const folder = path.join(SCREENSHOTS, 'phone');
  fs.mkdirSync(folder, { recursive: true });
  const { context, page, errors } = await mount(browser, { width: 360, height: 640 }, 3);
  await page.evaluate(() => {
    window.AndroidFiles = { systemInsets: () => JSON.stringify({ top: 24, right: 0, bottom: 24, left: 0 }) };
    applyNativeInsets();
  });
  assert.equal(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--native-safe-top').trim()), '24px');
  assert.ok(await page.evaluate(() => document.querySelector('.mobile-brand').getBoundingClientRect().top >= 24));
  assert.equal(await page.locator('.warehouse-map-card').isHidden(), true);
  assert.equal(await page.locator('.home-mobile-links').isVisible(), true);
  assert.equal(await page.locator('.home-history-card').isHidden(), true);
  assert.ok(await page.evaluate(() => document.querySelector('.home-mobile-links').getBoundingClientRect().top < document.querySelector('.home-metrics').getBoundingClientRect().top));
  assert.ok(await page.evaluate(() => [...document.querySelectorAll('.home-actions .btn,.home-mobile-link,.mobile-nav button')].every(element => element.getBoundingClientRect().height >= 44)));
  await shot(page, folder, '01-overview-1080x1920.png');
  await page.locator('.home-mobile-link[data-nav="history"]').click();
  assert.equal(await page.getAttribute('body', 'data-page'), 'history');
  await page.locator('.mobile-nav [data-nav="home"]').click();
  await page.locator('.home-mobile-link[data-action="open-shelf"]').click();
  assert.equal(await page.getAttribute('body', 'data-page'), 'shelf');
  assert.equal(await page.locator('.mobile-nav').isVisible(), false);
  assert.ok(await page.evaluate(() => document.querySelector('.shelf-back').getBoundingClientRect().top >= 24));
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  await shot(page, folder, '10-warehouse-map-1080x1920.png');
  await page.locator('.shelf-book').first().click();
  assert.equal(await page.locator('.shelf-item-page').isVisible(), true);
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  await shot(page, folder, '11-warehouse-item-1080x1920.png');
  await page.locator('[data-action="shelf-back"]').click();
  await page.locator('[data-action="shelf-back"]').click();
  await page.locator('.mobile-nav [data-nav="stock"]').click();
  await page.evaluate(() => {
    URL.createObjectURL = () => { throw new Error('legacy object URL path must not be used'); };
    HTMLImageElement.prototype.decode = () => Promise.reject(new Error('legacy image.decode path must not be used'));
  });
  await page.locator('[data-action="scan-code"]').first().click();
  await page.locator('#scan-image').setInputFiles(path.join(ROOT, 'tests', 'fixtures', 'bar-EAN13-460123456789.png'));
  await page.locator('#scan-use').click();
  await page.waitForFunction(() => document.querySelector('#scan-result')?.textContent.includes('4601234567893'));
  assert.match(await page.locator('#scan-status').textContent(), /не привязан/);
  await page.locator('#scan-close').click();
  await page.evaluate(() => {
    window.nativeCalls = [];
    window.AndroidFiles = {
      nativeScannerAvailable: () => true,
      scanCode: target => window.nativeCalls.push(target),
      systemInsets: () => JSON.stringify({ top: 24, right: 0, bottom: 24, left: 0 })
    };
    applyNativeInsets();
    newItemModal();
  });
  await page.locator('#item-form [data-action="scan-code"]').click();
  assert.deepEqual(await page.evaluate(() => window.nativeCalls), ['barcode']);
  assert.equal(await page.locator('.scan-viewport').isHidden(), true);
  await page.evaluate(() => YarusNativeScanner.deliver('4601234567893', 'EAN-13'));
  assert.equal(await page.locator('#item-form [name="barcode"]').inputValue(), '4601234567893');
  assert.equal(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--native-safe-top').trim()), '24px');
  assert.equal(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--native-safe-bottom').trim()), '24px');
  await page.locator('#item-form [data-action="scan-code"]').click();
  await page.evaluate(() => YarusNativeScanner.cancelled());
  assert.equal(await page.locator('#scan-root').count(), 0);
  await page.evaluate(() => { modalDirty = false; closeModal(true); delete window.AndroidFiles; });
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
  await page.evaluate(() => { window.AndroidFiles = { portableInfo: () => JSON.stringify({ attached: false, name: '', bytes: 0 }) }; render(); });
  await shot(page, folder, '09-portable-file-1080x1920.png', '.portable-box');
  await page.evaluate(() => accountModal('join'));
  const inviteScan = page.getByRole('button', { name: 'Сканировать QR камерой' });
  assert.equal(await inviteScan.isVisible(), true);
  await shot(page, folder, '08-qr-connect-1080x1920.png');
  await inviteScan.click();
  await page.waitForSelector('#scan-root');
  await page.locator('.scan-manual-details summary').click();
  await page.locator('#scan-manual').fill(await page.evaluate(() => YarusCodes.invite('http://192.168.100.3:8787', 'invite-token_1234567890', Math.floor(Date.now() / 1000) + 3600, 'A'.repeat(43))));
  await page.locator('#scan-manual-form button').click();
  assert.equal(await page.locator('#account-form [name="server"]').inputValue(), 'http://192.168.100.3:8787');
  assert.equal(await page.locator('#account-form [name="code"]').inputValue(), 'invite-token_1234567890');
  await page.locator('[data-action="close-modal"]').first().click();
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
  await page.evaluate(() => { window.AndroidFiles = { portableInfo: () => JSON.stringify({ attached: false, name: '', bytes: 0 }) }; render(); });
  await shot(page, folder, '06-portable-file-1600x2560.png', '.portable-box');
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

async function responsiveShelfChecks(browser) {
  for (const viewport of [
    { width: 320, height: 568 },
    { width: 360, height: 780 },
    { width: 834, height: 1112 },
    { width: 1112, height: 834 },
    { width: 1440, height: 1000 }
  ]) {
    const { context, page, errors } = await mount(browser, viewport, 1);
    await page.locator('[data-action="open-shelf"]:visible').click();
    assert.equal(await page.getAttribute('body', 'data-page'), 'shelf');
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    assert.equal(await page.evaluate(() => getComputedStyle(document.querySelector('.topbar')).display), 'none');
    const sizes = await page.locator('.shelf-book').evaluateAll(books => books.map(book => [book.offsetWidth, book.offsetHeight]));
    assert.ok(sizes.length >= 10);
    assert.ok(new Set(sizes.map(size => size.join('x'))).size > 1, 'book sizes must represent relative stock');
    await page.locator('[data-shelf-filter="attention"]').click();
    assert.equal(await page.locator('[data-shelf-filter="attention"]').getAttribute('class'), 'active');
    await page.locator('.shelf-book').first().click();
    assert.equal(await page.locator('.shelf-item-actions .btn').count(), 4);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await page.locator('.shelf-overflow summary').click();
    assert.equal(await page.locator('.shelf-overflow-menu').isVisible(), true);
    assert.deepEqual(errors, []);
    await context.close();
  }
}

(async () => {
  fs.mkdirSync(path.dirname(REPORT), { recursive: true });
  const browser = await chromium.launch({ executablePath: EDGE, headless: true, args: ['--disable-gpu'] });
  try {
    await phoneShots(browser);
    await tabletShotsAndReport(browser);
    await responsiveShelfChecks(browser);
  } finally {
    await browser.close();
  }
  console.log('PASS: responsive phone/tablet UI, QR preview, PDF workflow and RuStore screenshots');
})().catch(error => { console.error(error); process.exitCode = 1; });
