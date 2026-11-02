'use strict';
const assert=require('node:assert/strict');
const http=require('node:http');
const fs=require('node:fs');
const path=require('node:path');
const {chromium}=require('playwright');
const root=path.resolve(__dirname,'..'),web=path.join(root,'web');
const qa=path.join(root,'dist','QA','workspace-2.0');fs.mkdirSync(qa,{recursive:true});
const server=http.createServer((request,response)=>{
  const relative=new URL(request.url,'http://localhost').pathname;
  const file=path.resolve(web,'.'+(relative==='/'?'/index.html':relative));
  if(!file.startsWith(web+path.sep)){response.writeHead(403).end();return;}
  try{const body=fs.readFileSync(file);response.setHeader('Content-Type',file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':file.endsWith('.svg')?'image/svg+xml':'text/html; charset=utf-8');response.end(body);}catch{response.writeHead(404).end();}
});
const listen=()=>new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
async function openWork(page,tab){await page.evaluate(key=>{workTab=key;page='work';render();},tab);await page.waitForTimeout(100);}
(async()=>{
 await listen();const browser=await chromium.launch({executablePath:'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',headless:true});
 const context=await browser.newContext({viewport:{width:1440,height:1000},locale:'ru-RU',acceptDownloads:true});
 const page=await context.newPage(),errors=[];page.on('pageerror',error=>errors.push(String(error)));page.on('dialog',dialog=>dialog.accept());
 try{
  await page.goto('http://127.0.0.1:'+server.address().port);
  await page.getByRole('button',{name:/Сначала посмотреть на примере/}).click();await page.waitForSelector('.workspace-shortcuts');
  assert.equal(await page.evaluate(()=>activeItems().length),12);
  await page.evaluate(()=>{window.scrollTo(0,0);$('#toasts').innerHTML='';});await page.screenshot({path:path.join(qa,'desktop-overview.png'),fullPage:false});
  // The real IndexedDB profile survives reloads; optional settings never alter stock.
  const baseline=await page.evaluate(()=>JSON.stringify(state.stocks));
  await page.locator('.nav [data-nav="settings"]').click();
  await page.locator('[data-action="workspace-profile"][data-id="home"]').click();
  assert.equal(await page.evaluate(()=>moduleEnabled('prices')),false);
  assert.equal(await page.evaluate(()=>JSON.stringify(state.stocks)),baseline);
  await page.reload();await page.waitForSelector('#main');
  assert.equal(await page.evaluate(()=>cfg.profile),'home');
  await page.evaluate(()=>{page='home';render();});
  assert.equal(await page.locator('[data-widget="value"]').count(),0);
  await page.evaluate(()=>{cfg.widgets=['low'];saveCfg();render();});
  assert.equal(await page.locator('[data-widget="low"]').isVisible(),true);
  assert.equal(await page.locator('[data-widget="today"]').isHidden(),true);
  await page.evaluate(()=>{cfg.widgets=['items','value','low','today'];render();});
  // A completed count uses one atomic document, with untouched rows unchanged.
  await openWork(page,'inventory');
  await page.locator('#count-start [name="scope"]').selectOption('all');
  await page.locator('#count-start button').click();await page.waitForSelector('.count-value');
  const draft=await page.evaluate(()=>({first:countDraft.rows[0],second:countDraft.rows[1],before:D.historyCount(state),place:countDraft.place}));
  await page.locator('.count-value').first().fill('2');await page.waitForFunction(()=>document.querySelector('#count-save-status').textContent.includes('сохранён'));
  await page.reload();await page.waitForSelector('#main');await openWork(page,'inventory');await page.waitForSelector('.count-value');
  assert.equal(await page.locator('.count-value').first().inputValue(),'2');
  await page.evaluate(()=>{
    const original=putKV;let failOnce=true;
    putKV=async(key,value)=>{if(failOnce&&value?.command?.type==='batch'){failOnce=false;throw new Error('Имитация временной ошибки записи');}return original(key,value);};
  });
  await page.locator('#count-finish button').first().click();
  await page.waitForSelector('#count-review-submit');
  assert.ok((await page.locator('#modal-root').innerText()).includes('Непосчитанных:'));
  await page.setViewportSize({width:320,height:640});
  assert.equal(await page.evaluate(()=>document.querySelector('.modal').scrollWidth>innerWidth),false,'small phone review overflow');
  await page.screenshot({path:path.join(qa,'phone-count-review.png'),fullPage:false});
  await page.locator('#count-review-submit').click();
  await page.waitForFunction(()=>$('#toasts').textContent.includes('Имитация временной ошибки записи'));
  assert.equal(await page.evaluate(()=>D.historyCount(state)),draft.before,'unsaved document must never be sent');
  await page.locator('#count-finish button').first().click();
  await page.waitForFunction(()=>countDraft===null);
  await page.setViewportSize({width:1440,height:1000});
  assert.equal(await page.evaluate(args=>D.total(state,args.first.item,args.place),draft),2000);
  assert.equal(await page.evaluate(args=>D.total(state,args.second.item,args.place),draft),draft.second.qty);
  assert.equal(await page.evaluate(()=>D.historyCount(state)),draft.before+1);
  // A concurrently changed cell is refused, preserving the other participant's write.
  await openWork(page,'inventory');await page.locator('#count-start [name="scope"]').selectOption('all');await page.locator('#count-start button').click();
  await page.locator('.count-value').first().fill('4');await page.waitForFunction(()=>document.querySelector('#count-save-status').textContent.includes('сохранён'));
  await page.evaluate(async()=>{const row=countDraft.rows[0];await send({id:id(),type:'in',itemId:row.item,to:countDraft.place,qty:1000});});
  await page.locator('#count-finish button').first().click();await page.waitForTimeout(150);
  assert.equal(await page.evaluate(()=>D.total(state,countDraft.rows[0].item,countDraft.place)),3000);
  assert.ok(await page.locator('#toasts').innerText().then(text=>text.includes('Остатки изменились')));
  await page.locator('[data-action="count-cancel"]').click();await page.waitForFunction(()=>countDraft===null);
  // An acknowledged queued document closes its persisted draft, even if another
  // warehouse is now visible; its draft must remain untouched.
  const cleared=await page.evaluate(async()=>{
    const oldKey=workspaceStorageKey(),oldId=id(),otherKey=oldKey+':other';
    const old={id:oldId,command:{id:oldId,type:'batch'},rows:[]},other={id:id(),rows:[]};
    await putKV(oldKey,old);await putKV(otherKey,other);countDraftKey=otherKey;countDraft=other;
    await workspaceCommandAccepted(old.command,oldKey);
    const result={old:await getKV(oldKey),other:await getKV(otherKey),current:countDraft.id};
    await delKV(otherKey);countDraft=null;countDraftKey=oldKey;return {oldAbsent:result.old===undefined,otherPreserved:result.other.id===other.id,currentPreserved:result.current===other.id};
  });assert.deepEqual(cleared,{oldAbsent:true,otherPreserved:true,currentPreserved:true});
  await page.evaluate(async()=>{await send({id:id(),type:'item',item:{id:'unit-change-test',name:'Проверка единицы',sku:'UNIT-TEST',barcode:'',category:'Проверка единицы',unit:'шт',min:0,price:0,note:'',fields:{},archived:false,version:0}});});
  await openWork(page,'inventory');await page.locator('#count-start [name="scope"]').selectOption('all');await page.locator('#count-start [name="category"]').selectOption({label:'Проверка единицы'});await page.locator('#count-start button').click();
  await page.locator('.count-value').fill('3');await page.waitForFunction(()=>$('#count-save-status').textContent.includes('сохранён'));
  await page.evaluate(async()=>{await send({id:id(),type:'item',item:{...state.items['unit-change-test'],unit:'кг'}});});
  assert.equal(await page.locator('.count-entry small').innerText(),'шт','count must retain the original unit');
  await page.locator('#count-finish button').first().click();await page.waitForFunction(()=>$('#toasts').textContent.includes('Единица измерения изменилась'));
  assert.equal(await page.evaluate(()=>D.total(state,'unit-change-test')),0);
  await page.locator('[data-action="count-cancel"]').click();await page.waitForFunction(()=>countDraft===null);
  // A real bulk import becomes visible once, with true pagination beyond the old 300-row cap.
  await page.evaluate(()=>{page='stock';render();});await page.locator('[data-action="import"]').click();
  const csv='Название;Артикул;Категория;Единица;Минимум;Цена;Штрихкод\n'+Array.from({length:650},(_,n)=>`Материал ${String(n).padStart(4,'0')};BULK-${n};Большой каталог;шт;5;12;`).join('\n');
  await page.locator('#csv-file').setInputFiles({name:'catalog.csv',mimeType:'text/csv',buffer:Buffer.from(csv)});
  await page.waitForFunction(()=>document.querySelector('#csv-preview').textContent.includes('650 карточек'));
  await page.locator('button[form="import-form"]').click();await page.waitForFunction(()=>activeItems().length===663);
  await page.locator('[data-action="stock-page"]').last().click();
  const current=await page.evaluate(()=>stockPage);assert.ok(current>1);
  await page.locator('#stock-search').fill('Материал 0649');assert.ok(await page.locator('.inventory-table').innerText().then(text=>text.includes('Материал 0649')));
  // Replenishment export is a local file, not an outbound request.
  await openWork(page,'replenish');const downloaded=page.waitForEvent('download');await page.locator('[data-action="replenish-export"]').click();const file=await downloaded;assert.ok(file.suggestedFilename().includes('пополнение'));
  for(const [name,viewport] of [['phone',{width:390,height:844}],['small-phone',{width:320,height:640}],['tablet',{width:1024,height:1366}],['landscape',{width:1366,height:768}]]){
    await page.setViewportSize(viewport);await page.evaluate(()=>{page='home';render();window.scrollTo(0,0);$('#toasts').innerHTML='';});
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,name+' overflow');
    await page.screenshot({path:path.join(qa,name+'-overview.png'),fullPage:false});
    await page.evaluate(()=>{page='settings';render();});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,name+' settings overflow');
    await openWork(page,'replenish');assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,name+' work overflow');
  }
  // Final reload proves the bulk import was committed to actual browser storage.
  await page.reload();await page.waitForSelector('#main');assert.equal(await page.evaluate(()=>activeItems().length),663);
  assert.deepEqual(errors,[]);
  console.log('PASS: real IndexedDB reload, module profiles, correct widget toggles, durable count drafts, atomic count, conflict refusal, bulk import, catalog pagination, local CSV and responsive layouts.');
 }finally{await context.close();await browser.close();server.close();}
})().catch(error=>{console.error(error);server.close();process.exitCode=1;});
