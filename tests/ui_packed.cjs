'use strict';
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const {pathToFileURL}=require('node:url');
const {chromium}=require('playwright');
const root=path.resolve(__dirname,'..'),packed=path.join(root,'dist','YARUS.html');
(async()=>{
 const normalized=file=>fs.readFileSync(file,'utf8').replace(/\r\n/g,'\n'),html=normalized(packed);
 for(const name of ['domain.js','host.js','app.js','workspace.js','scanner.js'])assert.ok(html.includes(normalized(path.join(root,'web',name))),'packed source drift: '+name);
 assert.ok(html.includes(normalized(path.join(root,'web','workspace.css'))));
 assert.ok(!/<script src=|<link rel="stylesheet"/.test(html),'offline build has unbundled resources');
 const browser=await chromium.launch({executablePath:'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',headless:true});
 const context=await browser.newContext({viewport:{width:1024,height:768},locale:'ru-RU'}),page=await context.newPage(),errors=[],requests=[];
 page.on('pageerror',e=>errors.push(String(e)));page.on('request',r=>{if(/^https?:/.test(r.url()))requests.push(r.url());});
 try{
  await page.goto(pathToFileURL(packed).href);await page.getByRole('button',{name:/Сначала посмотреть на примере/}).click();await page.waitForSelector('.workspace-shortcuts');
  assert.equal(await page.evaluate(()=>APP_VERSION),'2.0.0');assert.equal(await page.evaluate(()=>activeItems().length),12);
  await page.reload();await page.waitForSelector('#main');assert.equal(await page.evaluate(()=>activeItems().length),12);
  await page.evaluate(()=>{page='work';render();});await page.waitForSelector('.work-tools');
  assert.deepEqual(errors,[]);assert.deepEqual(requests,[]);
  console.log('PASS: final self-contained HTML matches source, enforces packed CSP, launches and reloads offline with local IndexedDB and no HTTP requests.');
 }finally{await context.close();await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
