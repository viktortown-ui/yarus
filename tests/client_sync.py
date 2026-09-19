"""Two real client UIs against the real HTTP server.
Browser persistence is explicitly replaced by an in-memory adapter (see ui_smoke).
Transport delegates to urllib because sandbox browser navigation is policy-blocked.
This verifies client/server integration, not device networking or actual IndexedDB.
"""
import asyncio, json, os, pathlib, subprocess, tempfile, urllib.request, urllib.error, re
from playwright.async_api import async_playwright
from ui_smoke import mount
ROOT=pathlib.Path(__file__).resolve().parents[1]

def request(base,path,data=None,token=''):
 try:
  req=urllib.request.Request(base+path,data=None if data is None else json.dumps(data).encode(),headers={'Content-Type':'application/json',**({'Authorization':'Bearer '+token} if token else {})})
  with urllib.request.urlopen(req,timeout=5) as r:return {'status':r.status,'body':json.loads(r.read())}
 except urllib.error.HTTPError as e:return {'status':e.code,'body':json.loads(e.read())}

async def main():
 subprocess.run(['go','build','-o','/mnt/data/yarus-integration-server','.'],cwd=ROOT,check=True)
 reports=[]
 with tempfile.TemporaryDirectory() as tmp:
  import socket
  sock=socket.socket();sock.bind(('127.0.0.1',0));port=sock.getsockname()[1];sock.close();base=f'http://127.0.0.1:{port}'
  proc=subprocess.Popen(['/mnt/data/yarus-integration-server','--headless','--listen',f'127.0.0.1:{port}','--data',tmp],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
  try:
   setup=''
   for _ in range(15):
    line=proc.stdout.readline();m=re.search(r'setup=([^\s]+)',line)
    if m:setup=m[1];break
   assert setup
   owner=request(base,'/api/setup',{'setup':setup,'login':'owner','name':'Владелец','password':'testpass-12345','space':'Интеграционный склад'})
   assert owner['status']==200,owner;oa=owner['body'];invite=request(base,'/api/invite',{'role':'editor'},oa['token'])
   assert invite['status']==200,invite
   other=request(base,'/api/join',{'code':invite['body']['code'],'login':'worker','name':'Кладовщик','password':'testpass-12345'})
   assert other['status']==200,other;ob=other['body']
   async with async_playwright() as pw:
    browser=await pw.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox'])
    pages=[];allerrors=[]
    for auth in [oa,ob]:
     page,errors=await mount(browser);allerrors.append(errors)
     await page.expose_function('transport',lambda path,data,token:request(base,path,data,token))
     await page.evaluate('''async ({base,auth})=>{
      window.testOffline=false;window.loseNextAck=false;
      api=async (path,data,baseArg=cfg.server,token=cfg.token)=>{
       if(window.testOffline)throw Object.assign(new Error('Test offline'),{network:true});
       const r=await window.transport(path,data===undefined?null:data,token);
       if(window.loseNextAck&&path==='/api/command'){window.loseNextAck=false;throw Object.assign(new Error('Test lost ACK'),{network:true});}
       if(r.status!==200)throw Object.assign(new Error(r.body.error),{status:r.status});return r.body;
      };
      cfg={mode:'remote',server:base,token:auth.token,login:auth.state.me.login};state=null;queue=[];await acceptState(auth.state);network='online';render();
     }''',{'base':base,'auth':auth});pages.append(page)
    a,b=pages
    await a.evaluate("send({id:id(),type:'item',item:{id:'sync-product',name:'Болт',sku:'SYNC',barcode:'',category:'Крепёж',unit:'шт',min:0,price:100,fields:{},note:'',archived:false,version:0}})")
    await a.evaluate("send({id:id(),type:'in',itemId:'sync-product',to:'main-place',qty:5000})")
    await b.evaluate('syncNow()');assert await b.evaluate("D.total(state,'sync-product')")==5000
    reports.append('PASS: second authenticated client receives first client\'s item and stock through real HTTP')
    await b.evaluate("testOffline=true;send({id:id(),type:'out',itemId:'sync-product',from:'main-place',qty:4000})")
    assert await b.evaluate('queue.length')==1;assert await b.evaluate("D.total(state,'sync-product')")==5000
    reports.append('PASS: offline draft is saved in explicit test storage adapter, does not alter confirmed stock')
    await a.evaluate("send({id:id(),type:'out',itemId:'sync-product',from:'main-place',qty:4000})")
    await b.evaluate('testOffline=false;syncNow()')
    assert await b.evaluate("D.total(state,'sync-product')")==1000
    assert await b.evaluate("queue.length===1&&queue[0].error.includes('Недостаточно')")
    reports.append('PASS: reconnecting client sees remaining 1 and a visible conflict; failed draft is retained')
    before=await a.evaluate('state.eventCount')
    await a.evaluate("loseNextAck=true;send({id:id(),type:'in',itemId:'sync-product',to:'main-place',qty:2000})")
    assert await a.evaluate('queue.length')==1
    await a.evaluate('syncNow()')
    assert await a.evaluate('queue.length')==0
    assert await a.evaluate('state.eventCount')==before+1
    assert await a.evaluate("D.total(state,'sync-product')")==3000
    reports.append('PASS: a deliberately lost HTTP acknowledgement is retried exactly once without duplicate movement')
    await a.evaluate("send({id:id(),type:'place',place:{id:'second-location',name:'Цех',note:'',version:0}})")
    await a.evaluate("send({id:id(),type:'transfer',itemId:'sync-product',from:'main-place',to:'second-location',qty:1000})")
    await b.evaluate('syncNow()');assert await b.evaluate("D.total(state,'sync-product','second-location')")==1000
    reports.append('PASS: transfer is reflected consistently on both authenticated clients')
    # Client URL normalization rejects domain-name masquerading as private addresses.
    assert await a.evaluate("(()=>{try{normalizeServer('http://192.168.attacker.example');return false}catch{return true}})()")
    assert await a.evaluate("normalizeServer('192.168.1.20:8787')")=='http://192.168.1.20:8787'
    reports.append('PASS: cleartext restricted to literal private IPs/localhost; fake private-looking domains rejected')
    assert all(not e for e in allerrors),allerrors
    reports.append('PASS: no uncaught JavaScript errors in two-client flows')
    await browser.close()
  finally:proc.terminate();proc.wait(timeout=5)
 report='\n'.join(reports)+'\n\nReal HTTP + real server journal; client persistence uses explicit in-memory adapter. No native-device validation.\n'
 (ROOT/'docs/client-sync-tests.txt').write_text(report);print(report)
asyncio.run(main())
