'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {chromium}=require('playwright');
const ROOT=path.resolve(__dirname,'..'),source=name=>fs.readFileSync(path.join(ROOT,'web',name),'utf8');
const ADAPTER=`
const mem=new Map();
openDB=async()=>({});getKV=async k=>mem.has(k)?D.copy(mem.get(k)):undefined;
putKV=async(k,v)=>{mem.set(k,D.copy(v));};delKV=async k=>{mem.delete(k);};
applyLocal=async c=>{const result=D.execute(await getKV(activeKey())||state,c);await putKV(activeKey(),result);return result;};
mutateQueue=async edit=>{queue=edit(await getKV(queueKey())||[]);await putKV(queueKey(),queue);return queue;};
saveCfg=()=>{};inferServer=()=>'';init();`;
async function mount(browser){
 const context=await browser.newContext({viewport:{width:1360,height:900},locale:'ru-RU'}),page=await context.newPage(),errors=[];
 page.on('pageerror',e=>errors.push(String(e)));
 let html=source('index.html').replace(/<link[^>]+rel="stylesheet"[^>]*>/g,'').replace(/<script src="[^"]+"><\/script>/g,'').replace(/<meta http-equiv="Content-Security-Policy"[^>]+>/g,'');
 await page.setContent(html);await page.addStyleTag({content:source('style.css')+'\n'+source('workspace.css')});
 for(const name of ['domain.js','transport.js','host.js','transfer.js','vendor/qr-encode.js','vendor/qr-decode.js','vendor/code128-patterns.js','codes.js','reports.js','scanner.js','workspace.js'])await page.addScriptTag({content:source(name)});
 const app=source('app.js').trimEnd();assert.ok(app.endsWith('init();'));await page.addScriptTag({content:app.slice(0,-7)+ADAPTER});
 await page.getByRole('button',{name:/Сначала посмотреть на примере/}).click();await page.waitForSelector('#main');return {context,page,errors};
}
(async()=>{
 const browser=await chromium.launch({executablePath:'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',headless:true,args:['--disable-gpu']});
 try{
  const {context,page,errors}=await mount(browser);
  await page.evaluate(async()=>{for(let n=0;n<240;n++){const key='pagination-item-'+String(n).padStart(4,'0');state.items[key]={id:key,name:'Позиция '+String(n).padStart(4,'0'),sku:'PAGE-'+n,barcode:'',category:'Проверка',unit:'шт',min:0,price:100,note:'',fields:{},archived:false,version:1};}await putKV(activeKey(),state);page='stock';render();});
  assert.equal(await page.locator('.inventory-table tbody tr').count(),60);
  await page.getByRole('button',{name:'Следующая страница'}).click();assert.match(await page.locator('.page-position').innerText(),/2 \/ 5/);
  await page.locator('#stock-search').fill('PAGE-239');assert.equal(await page.locator('.inventory-table tbody tr').count(),1);assert.match(await page.locator('.page-position').innerText(),/1 \/ 1/);
  await page.evaluate(()=>{search='';cfg.pageSize=30;render();});assert.equal(await page.locator('.inventory-table tbody tr').count(),30);
  await page.locator('[data-action="new-item"]').first().click();await page.locator('[name="name"]').fill('Карточка с характеристиками');await page.locator('.item-more>summary').click();
  await page.locator('[data-action="field-add"]').click();await page.locator('.custom-field-name').fill('Материал');await page.locator('.custom-field-value').fill('Нержавеющая сталь');
  await page.locator('button[form="item-form"]').click();await page.waitForSelector('#item-form',{state:'detached'});
  assert.equal(await page.evaluate(()=>activeItems().find(x=>x.name==='Карточка с характеристиками').fields.Материал),'Нержавеющая сталь');
  await page.evaluate(()=>detailModal('demo-item-0'));await page.locator('[data-action="copy-item"]').click();assert.match(await page.locator('.modal-header h2').innerText(),/по образцу/);assert.equal(await page.locator('#item-form [name="sku"]').inputValue(),'');assert.equal(await page.locator('#item-form [name="barcode"]').inputValue(),'');
  const beforeCopy=await page.evaluate(()=>activeItems().length);await page.locator('#item-form [name="name"]').fill('Новая похожая позиция');await page.locator('button[form="item-form"]').click();await page.waitForSelector('#item-form',{state:'detached'});assert.equal(await page.evaluate(()=>activeItems().length),beforeCopy+1);
  await page.evaluate(()=>operationModal('in','pagination-item-0239'));assert.ok(await page.locator('#op-item option').count()<=100);assert.equal(await page.locator('#op-item').inputValue(),'pagination-item-0239');
  await page.locator('#op-qty').fill('2,5');assert.match(await page.locator('#quantity-after').innerText(),/2,5 шт/);
  await page.locator('#op-find').fill('PAGE-220');assert.ok(await page.locator('#op-item option').count()<=2);await page.locator('#op-item').selectOption('pagination-item-0220');assert.equal(await page.locator('#op-item').inputValue(),'pagination-item-0220');
  await page.evaluate(()=>{closeModal(true);const item=activeItems()[0];for(let n=0;n<140;n++)state=D.execute(state,{id:D.newId(),type:'in',itemId:item.id,to:'main-place',qty:1000,note:n===17?'Искомый документ':'Проверка'});page='history';render();});
  assert.equal(await page.locator('.history-list>.activity-row').count(),50);await page.getByRole('button',{name:'Следующая страница'}).click();assert.match(await page.locator('.page-position').innerText(),/2 \/ 4/);
  await page.locator('#history-search').fill('Искомый документ');assert.equal(await page.locator('.history-list>.activity-row').count(),1);assert.match(await page.locator('.page-position').innerText(),/1 \/ 1/);
  await page.locator('#history-from').fill('2099-01-01');assert.equal(await page.locator('.history-list>.activity-row').count(),0);await page.locator('[data-action="history-reset"]').click();assert.equal(await page.locator('.history-list>.activity-row').count(),50);
  await page.evaluate(()=>{for(let n=0;n<120;n++)state.places['pagination-place-'+n]={id:'pagination-place-'+n,name:'Место '+String(n).padStart(3,'0'),note:'',version:1};page='places';render();});assert.equal(await page.locator('.place-card').count(),48);
  await page.locator('#places-search').fill('Место 119');assert.equal(await page.locator('.place-card').count(),1);
  const sparseExport=await page.evaluate(()=>{let exported='';const save=downloadText;downloadText=(name,text)=>{exported=text;};exportCSV();downloadText=save;return {rows:D.parseCSV(exported).length,items:activeItems().length,stocks:Object.values(state.stocks).filter(x=>x.qty>0).length};});assert.ok(sparseExport.rows<=sparseExport.items+sparseExport.stocks+1,'CSV must not expand empty item/place combinations');
  await page.evaluate(()=>{queue=[{command:{id:'bulk-test-command',type:'catalog-import',items:Array.from({length:4},()=>({}))},createdAt:new Date().toISOString(),error:''}];queueModal();});assert.match(await page.locator('.queue-item h3').innerText(),/Импорт каталога/);assert.match(await page.locator('.queue-item p').first().innerText(),/4 строки/);await page.evaluate(()=>{queue=[];closeModal(true);});
  await page.evaluate(()=>{cfg.modules={scanner:false,prices:false,fields:true};page='stock';render();newItemModal();});assert.equal(await page.locator('#item-form [data-action="scan-code"]').isVisible(),false);assert.equal(await page.locator('#item-form [name="price"]').isVisible(),false);
  await page.evaluate(()=>{closeModal(true);page='settings';render();});assert.doesNotMatch(await page.locator('#storage-info').innerText(),/Считаем/);
  for(const width of [320,360,768,1360]){await page.setViewportSize({width,height:900});for(const target of ['stock','history','places']){await page.evaluate(target=>{page=target;render();window.scrollTo(0,0);},target);assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'overflow '+target+' '+width);}}
  assert.deepEqual(errors,[]);await context.close();console.log('PASS: catalogue/history/place pagination, filtered history, guided fields, bounded movement chooser, module visibility, local storage info, responsive layouts');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
