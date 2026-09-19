"""UI tests in Chromium with an explicit memory storage adapter.
The sandbox browser blocks URL navigation by policy. This test does not change
browser policies. It uses set_content and injects the unchanged app code, then
replaces ONLY persistence/network transport with test adapters. Go tests cover
real HTTP and persistence separately. This is not a native-device test.
"""
import asyncio, json, re, pathlib, urllib.request, urllib.error
from playwright.async_api import async_playwright
ROOT=pathlib.Path(__file__).resolve().parents[1]
OUT=ROOT/'docs'/'screenshots';OUT.mkdir(parents=True,exist_ok=True)

ADAPTER='''
const mem=new Map();
openDB=async()=>({});getKV=async k=>mem.has(k)?D.copy(mem.get(k)):undefined;
putKV=async(k,v)=>{mem.set(k,D.copy(v));};delKV=async k=>{mem.delete(k);};
applyLocal=async c=>{const result=D.execute(await getKV(activeKey())||state,c);await putKV(activeKey(),result);return result;};
mutateQueue=async edit=>{queue=edit(await getKV(queueKey())||[]);await putKV(queueKey(),queue);return queue;};
saveCfg=()=>{};inferServer=()=>'';
init();
'''
async def mount(browser,width=1440,height=1000):
 page=await browser.new_page(viewport={'width':width,'height':height},device_scale_factor=1)
 errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
 html=(ROOT/'web'/'index.html').read_text().replace('<link rel="stylesheet" href="style.css">','<style>'+(ROOT/'web'/'style.css').read_text()+'</style>')
 html=re.sub(r'<script src="[^"]+"></script>', '', html)
 html=re.sub(r'<meta http-equiv="Content-Security-Policy"[^>]+>', '', html)
 await page.set_content(html)
 for name in ['domain.js','vendor/qr-encode.js','vendor/qr-decode.js','vendor/code128-patterns.js','codes.js','scanner.js']:
  await page.add_script_tag(content=(ROOT/'web'/name).read_text())
 js=(ROOT/'web'/'app.js').read_text();assert js.rstrip().endswith('init();');js=js.rstrip()[:-7]+ADAPTER
 await page.add_script_tag(content=js)
 await page.wait_for_selector('.welcome')
 return page,errors

async def main():
 reports=[]
 async with async_playwright() as p:
  browser=await p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox'])
  page,errors=await mount(browser)
  await page.get_by_role('button',name='Сначала посмотреть на примере').click()
  await page.wait_for_selector('#main')
  assert await page.evaluate('activeItems().length')==12
  await page.screenshot(path=str(OUT/'desktop-overview.png'),full_page=True)
  reports.append('PASS: demo creates 12 products and independent demo workspace')
  await page.locator('.nav [data-nav="stock"]').click()
  await page.screenshot(path=str(OUT/'desktop-stock.png'),full_page=True)
  await page.locator('#stock-search').fill('Канистра')
  assert await page.locator('.inventory-table tbody tr').count()==1
  await page.locator('.inventory-table tbody tr').click()
  assert await page.locator('#modal-root').get_by_text('Канистра 5 л',exact=True).count()==1
  await page.locator('[data-action="close-modal"]').first.click()
  await page.locator('#stock-search').fill('')
  await page.locator('[data-action="new-item"]').click()
  await page.locator('[name="name"]').fill('Тестовая гайка')
  await page.locator('[name="sku"]').fill('QA-123')
  await page.locator('[name="category"]').fill('Контроль')
  await page.locator('[name="min"]').fill('2')
  await page.locator('button[form="item-form"]').click()
  await page.wait_for_selector('.modal',state='detached')
  assert await page.evaluate('activeItems().length')==13
  reports.append('PASS: search, item detail and item creation through visible controls')
  await page.locator('#stock-search').fill('Тестовая гайка')
  await page.locator('.inventory-table tbody tr').click()
  await page.locator('#modal-root [data-action="operation"][data-kind="in"]').click()
  await page.locator('#op-qty').fill('5,125')
  await page.locator('button[form="operation-form"]').click()
  await page.wait_for_selector('.modal',state='detached')
  assert await page.evaluate("D.total(state,activeItems().find(x=>x.sku==='QA-123').id)")==5125
  reports.append('PASS: fractional receipt 5.125 updates exact integer stock')
  await page.locator('.inventory-table tbody tr').click()
  await page.locator('#modal-root [data-action="operation"][data-kind="out"]').click()
  await page.locator('#op-qty').fill('6')
  await page.locator('button[form="operation-form"]').click()
  await page.wait_for_function("document.querySelector('#quantity-error').textContent.includes('не хватает')")
  assert await page.evaluate("D.total(state,activeItems().find(x=>x.sku==='QA-123').id)")==5125
  page.on('dialog',lambda d:d.accept())
  await page.locator('[data-action="close-modal"]').first.click()
  reports.append('PASS: insufficient stock shown as a visible error, no false success')
  await page.locator('#stock-search').fill('')
  for width,height,label in [(834,1112,'tablet'),(390,844,'mobile'),(360,780,'small-phone')]:
   await page.set_viewport_size({'width':width,'height':height})
   await page.evaluate("document.querySelector('#toasts').innerHTML='';scrollTo(0,0)")
   await page.evaluate("page='home';render()")
   await page.screenshot(path=str(OUT/(label+'-overview.png')),full_page=False)
   assert not await page.evaluate('document.documentElement.scrollWidth>innerWidth'),label+' overflow overview'
   await page.evaluate("page='stock';render()")
   assert not await page.evaluate('document.documentElement.scrollWidth>innerWidth'),label+' overflow stock'
   await page.screenshot(path=str(OUT/(label+'-stock.png')),full_page=False)
   reports.append(f'PASS: {label} {width}×{height}, no horizontal page overflow')
  await page.evaluate("page='settings';render()")
  await page.locator('#theme-select').select_option('dark')
  assert await page.evaluate('document.documentElement.dataset.theme')=='dark'
  await page.screenshot(path=str(OUT/'mobile-dark.png'),full_page=True)
  reports.append('PASS: theme switching and mobile settings')
  assert not errors,errors
  reports.append('PASS: no uncaught JavaScript errors in exercised UI flows')
  await browser.close()
 text='\n'.join(reports)+'\n\nStorage: explicit in-memory test adapter. Native IndexedDB/device execution not tested in this UI run.\n'
 (ROOT/'docs'/'ui-tests.txt').write_text(text)
 print(text)
if __name__=='__main__':asyncio.run(main())
