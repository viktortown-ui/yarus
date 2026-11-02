'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');

function device(){
 let stored='',writes=0;
 const context={crypto:global.crypto,TextEncoder,TextDecoder,Uint8Array,setTimeout,clearTimeout,btoa,atob,console};context.window=context;
 context.AndroidFiles={loadHostState:()=>stored,saveHostState:value=>(stored=value,writes++,true),hostStateSize:()=>Buffer.byteLength(stored),startHostServer:()=>JSON.stringify(['http://192.168.1.25:8787']),stopHostServer:()=>{},hostRespond:()=>{}};
 vm.createContext(context);for(const name of ['domain.js','transport.js','host.js'])vm.runInContext(fs.readFileSync(path.join(__dirname,'../web',name),'utf8'),context);
 return {host:context.YarusAndroidHost,writes:()=>writes,state:()=>JSON.parse(stored),replaceState:value=>{stored=JSON.stringify(value);context.YarusAndroidHost.load();}};
}

test('Android host protects hidden prices on catalog edits and new cards',async()=>{
 const d=device(),H=d.host,created=await H.create({space:'Мастерская',device:'Планшет',login:'owner',name:'Владелец',password:'owner-password-123'}),authorization='Bearer '+created.token;
 const item={id:'hidden-price-item1',name:'Материал',sku:'HIDDEN-1',barcode:'',category:'',unit:'шт',min:0,price:12345,note:'',fields:{},archived:false,version:0};
 await H.request('/api/command',{authorization,body:{id:'hidden-price-create',type:'item',item}});
 const invite=await H.request('/api/invite',{authorization,body:{role:'manager'}}),joined=await H.request('/api/join',{body:{code:invite.code,login:'editor',name:'Менеджер',password:'editor-password-123'}}),editor='Bearer '+joined.token;
 const stored=d.state();stored.users[joined.state.me.id].permissions={view_prices:false};d.replaceState(stored);
 const snapshot=await H.request('/api/state',{authorization:editor});assert.equal(snapshot.items[item.id].price,0);
 const edited=await H.request('/api/command',{authorization:editor,body:{id:'hidden-price-rename',type:'item',item:{...snapshot.items[item.id],name:'Новое название'}}});assert.equal(edited.state.items[item.id].price,0);assert.equal(d.state().items[item.id].price,12345);
 await H.request('/api/command',{authorization:editor,body:{id:'hidden-price-new01',type:'item',item:{...item,id:'hidden-price-item2',sku:'HIDDEN-2',price:99999}}});assert.equal(d.state().items['hidden-price-item2'].price,0);
 await H.request('/api/command',{authorization:editor,body:{id:'hidden-price-bulk1',type:'catalog-import',items:[{...item,id:'hidden-price-item3',sku:'HIDDEN-3',price:99999}]}});assert.equal(d.state().items['hidden-price-item3'].price,0);
});

test('Android host imports 10k cards with one save and persists each stock document atomically',async()=>{
 const d=device(),H=d.host,created=await H.create({space:'Мастерская',device:'Планшет',login:'owner',name:'Владелец',password:'owner-password-123'}),authorization='Bearer '+created.token;
 assert.equal(created.state.limits.items,50000);assert.equal(created.state.capabilities.batch,true);assert.equal(created.state.capabilities.catalogImport,true);
 const items=Array.from({length:10000},(_,i)=>({id:'android-bulk-'+String(i).padStart(6,'0'),name:'Позиция '+i,sku:'M-'+i,barcode:'',category:'',unit:'шт',min:0,price:0,note:'',fields:{},archived:false,version:0}));
 const command={id:'android-bulk-import',type:'catalog-import',items},writes=d.writes();
 const result=await H.request('/api/command',{authorization,body:command});assert.equal(Object.keys(result.state.items).length,10000);assert.equal(d.writes(),writes+1);
 const duplicate=await H.request('/api/command',{authorization,body:command});assert.equal(duplicate.duplicate,true);assert.equal(d.writes(),writes+1);
 const batch={id:'android-stock-batch',type:'batch',ref:'Поставка 12',commands:[
  {id:'android-stock-row1',type:'in',itemId:items[0].id,to:'main-place',qty:5000},
  {id:'android-stock-row2',type:'out',itemId:items[0].id,from:'main-place',qty:6000}
 ]},before=d.state();
 await assert.rejects(()=>H.request('/api/command',{authorization,body:batch}),/Недостаточно/);assert.deepEqual(d.state(),before);assert.equal(d.writes(),writes+1);
 batch.commands[1].qty=2000;const receipt=await H.request('/api/command',{authorization,body:batch});assert.equal(receipt.state.stocks[items[0].id+'@main-place'].qty,3000);assert.equal(d.writes(),writes+2);
 assert.equal(receipt.state.events[0].seq,receipt.state.events[1].seq);assert.equal(receipt.state.events[0].ref,'Поставка 12');
 const retried=await H.request('/api/command',{authorization,body:batch});assert.equal(retried.duplicate,true);assert.equal(d.writes(),writes+2);
});
