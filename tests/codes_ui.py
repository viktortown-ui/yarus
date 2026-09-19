"""Common UI regression tests. Storage/permissions are explicit test adapters.
No actual camera, native file dialog or device IndexedDB is claimed here.
"""
import asyncio, pathlib, json, base64
from playwright.async_api import async_playwright
from ui_smoke import mount
ROOT=pathlib.Path(__file__).resolve().parents[1]
OUT=ROOT/'docs'/'screenshots'
async def main():
 reports=[]
 async with async_playwright() as pw:
  browser=await pw.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox'])
  p,errors=await mount(browser,390,844)
  p.on('dialog',lambda d:d.accept())
  await p.get_by_role('button',name='Сначала посмотреть на примере').click()
  await p.wait_for_selector('#main')
  await p.evaluate("page='stock';render(); window.exports=[]; downloadText=(name,text,mime)=>window.exports.push({name,text,mime});")
  before=await p.evaluate('state.eventCount')
  stock=await p.evaluate('JSON.stringify(state.stocks)')
  await p.locator('[data-action="scan-code"]').first.click()
  await p.locator('#scan-manual').fill('4601234567893')
  await p.locator('#scan-manual-form button').click()
  await p.locator('#scan-bind').click()
  await p.locator('#bind-item').select_option('demo-item-2')
  await p.locator('button[form="bind-code-form"]').click()
  await p.wait_for_selector('#bind-code-form',state='detached')
  assert await p.evaluate("state.items['demo-item-2'].barcode")=='4601234567893'
  assert await p.evaluate('state.eventCount')==before
  assert await p.evaluate('JSON.stringify(state.stocks)')==stock
  reports.append('PASS: unknown EAN bound through controls without changing stock/history')
  await p.locator('[data-action="scan-code"]').first.click()
  await p.locator('#scan-image').set_input_files(str(ROOT/'tests/fixtures/bar-EAN13-460123456789.png'))
  await p.wait_for_selector('#scan-root',state='detached')
  assert await p.locator('#modal-root').get_by_text('Канистра 5 л',exact=True).count()==1
  reports.append('PASS: uploaded EAN image decoded offline, opens the linked item')
  await p.locator('[data-action="label-item"]').click()
  await p.locator('#label-copies').fill('12')
  await p.locator('#label-pdf').click()
  exp=await p.evaluate('window.exports.at(-1)')
  assert exp['mime']=='application/pdf'
  (ROOT/'docs/label-a4-example.pdf').write_text(exp['text'],encoding='ascii')
  # Decode the actual preview rendered for the actual product, not a different fixture.
  qr=await p.evaluate("YarusCodes.decode(document.querySelector('#label-preview canvas').getContext('2d').getImageData(0,0,840,480))")
  expected=await p.evaluate("YarusCodes.internal(state.space.id,'item','demo-item-2')")
  assert qr['text']==expected,(qr,expected)
  await p.locator('#label-paper').select_option('single')
  await p.locator('#label-pdf').click()
  exp=await p.evaluate('window.exports.at(-1)')
  (ROOT/'docs/label-70x40-example.pdf').write_text(exp['text'],encoding='ascii')
  await p.locator('#label-svg').click()
  exp=await p.evaluate('window.exports.at(-1)')
  assert '<svg' in exp['text'] and 'script' not in exp['text']
  (ROOT/'docs/qr-example.svg').write_text(exp['text'])
  await p.screenshot(path=str(OUT/'mobile-label.png'))
  reports.append('PASS: QR preview round-trip and real UI exports: 12-label A4 PDF, single 70x40 PDF and SVG')
  await p.locator('[data-action="close-modal"]').first.click()
  await p.evaluate("operationModal('out','demo-item-2')")
  await p.locator('#op-qty').fill('17')
  await p.locator('.scan-field-btn').first.click()
  await p.locator('#scan-manual').fill(expected)
  await p.locator('#scan-manual-form button').click()
  await p.wait_for_selector('#scan-root',state='detached')
  assert await p.locator('#op-qty').input_value()=='17'
  assert await p.locator('#op-item').input_value()=='demo-item-2'
  assert await p.evaluate('state.eventCount')==before
  available=await p.evaluate("(state.stocks[D.key('demo-item-2',document.querySelector('#op-place').value)]?.qty||0)/1000")
  await p.locator('#op-qty').fill(str(available+1))
  await p.wait_for_function("document.querySelector('#quantity-error').textContent.includes('не хватает')")
  assert await p.locator('#op-qty').get_attribute('aria-invalid')=='true'
  await p.locator('button[form="operation-form"]').click()
  assert await p.evaluate('state.eventCount')==before
  await p.screenshot(path=str(OUT/'mobile-shortage.png'))
  reports.append('PASS: operation scan preserves typed quantity; insufficient stock shows immediate explanation and blocks commit')
  await p.locator('[data-action="close-modal"]').first.click()
  await p.locator('[data-action="scan-code"]').first.click()
  await p.locator('#scan-manual').fill('YARUS:1:foreign-space:item:demo-item-2')
  await p.locator('#scan-manual-form button').click()
  assert 'друг' in (await p.locator('#scan-status').text_content()).lower()
  await p.locator('#scan-manual').fill('https://example.org/not-opened')
  await p.locator('#scan-manual-form button').click()
  assert p.url=='about:blank'
  assert await p.locator('#scan-create').count()==1
  await p.locator('#scan-close').click()
  reports.append('PASS: foreign warehouse QR refused; URL QR treated as text, no navigation')
  # Explicit camera capability/permission adapter; does not claim physical-camera test.
  await p.evaluate("""Object.defineProperty(window,'isSecureContext',{configurable:true,value:true});
  Object.defineProperty(navigator,'mediaDevices',{configurable:true,value:{getUserMedia:async()=>{const e=new Error('permission test');e.name='NotAllowedError';throw e;}}});""")
  await p.locator('[data-action="scan-code"]').first.click()
  await p.locator('#scan-start').click()
  await p.wait_for_function("document.querySelector('#scan-status').textContent.includes('не разрешён')")
  assert await p.locator('#scan-start').is_enabled()
  reports.append('PASS: simulated permission refusal gives an explanation and manual/image alternatives')
  for width,height,label in [(360,780,'small-phone'),(834,1112,'tablet'),(1112,834,'tablet-landscape'),(1440,1000,'desktop')]:
   await p.set_viewport_size({'width':width,'height':height})
   await p.evaluate("document.documentElement.dataset.theme='dark'")
   assert not await p.evaluate('document.documentElement.scrollWidth>innerWidth')
   assert await p.locator('#scan-root').is_visible()
   await p.screenshot(path=str(OUT/(label+'-scanner-dark.png')))
   reports.append(f'PASS: scanner layout {width}x{height}, dark theme, no horizontal page overflow')
  await p.locator('#scan-close').click()
  await p.evaluate("openScanner('lookup')")
  placeQR=await p.evaluate("YarusCodes.internal(state.space.id,'place','workshop-place')")
  await p.locator('#scan-manual').fill(placeQR)
  await p.locator('#scan-manual-form button').click()
  await p.wait_for_selector('#scan-root',state='detached')
  assert await p.evaluate('placeFilter')=='workshop-place'
  assert await p.evaluate('state.eventCount')==before
  reports.append('PASS: place QR applies location filter without inventory movement')
  await p.evaluate("state.me.role='viewer';openScanner('lookup')")
  await p.locator('#scan-manual').fill('UNLINKED-123')
  await p.locator('#scan-manual-form button').click()
  assert await p.locator('#scan-create').count()==0 and await p.locator('#scan-bind').count()==0
  reports.append('PASS: read-only participant cannot create or bind an unknown code')
  assert not errors,errors
  await browser.close()
 text='\n'.join(reports)+'\nNOT TESTED: real camera, native dialogs, IndexedDB; explicit adapters in this run.\n'
 (ROOT/'docs/codes-ui-tests.txt').write_text(text);print(text)
if __name__=='__main__':asyncio.run(main())
