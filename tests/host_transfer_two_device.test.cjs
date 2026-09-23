'use strict';
const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const {webcrypto}=require('node:crypto');

const ROOT=path.resolve(__dirname,'..');
const source=name=>fs.readFileSync(path.join(ROOT,'web',name),'utf8');

function device(address){
 let stored='';
 const context={crypto:webcrypto,TextEncoder,TextDecoder,URL,atob,btoa,setTimeout,clearTimeout,console};
 context.globalThis=context;
 context.AndroidFiles={
  loadHostState:()=>stored,
  saveHostState:value=>(stored=value,true),
  hostStateSize:()=>Buffer.byteLength(stored),
  startHostServer:()=>JSON.stringify([address]),
  stopHostServer:()=>{},
  hostRespond:()=>{}
 };
 vm.createContext(context);
 for(const name of ['domain.js','transport.js','host.js'])vm.runInContext(source(name),context,{filename:name});
 return context.YarusAndroidHost;
}

test('Android main-device transfer activates only after both devices confirm',async()=>{
 const oldDevice=device('http://192.168.1.20:8787');
 const created=await oldDevice.create({space:'Мастерская',device:'Старый планшет',login:'owner',name:'Владелец',password:'owner-password-123'});
 const oldAuth='Bearer '+created.token;
 await oldDevice.request('/api/command',{method:'POST',authorization:oldAuth,body:{id:'catalog-command-0001',type:'item',item:{id:'product-transfer-01',name:'Станок',sku:'ST-1',barcode:'',category:'Оборудование',unit:'шт',min:0,price:10_000_000_000_000,note:'',fields:{},archived:false,version:0}}});
 await oldDevice.request('/api/command',{method:'POST',authorization:oldAuth,body:{id:'movement-command-001',type:'in',itemId:'product-transfer-01',to:'main-place',qty:1000}});

 const transfer=await oldDevice.transferState('Новый телефон');
 assert.equal(oldDevice.info().host.status,'transfer_pending');
 assert.equal(transfer.state.host.status,'awaiting_activation');
 assert.equal(transfer.state.transfer.activationToken,undefined);

 const newDevice=device('http://192.168.1.44:8787');
 const imported=await newDevice.importTransfer(transfer);
 assert.equal(imported.state.host.status,'awaiting_activation');
 assert.equal(imported.state.stocks['product-transfer-01@main-place'].qty,1000);
 const newAuth='Bearer '+imported.token;
 await assert.rejects(()=>newDevice.request('/api/transfer/activate',{method:'POST',authorization:newAuth,body:{activationToken:'wrong-token'}}),/не подходит/);
 await assert.rejects(()=>newDevice.request('/api/command',{method:'POST',authorization:newAuth,body:{id:'movement-command-002',type:'out',itemId:'product-transfer-01',from:'main-place',qty:1000}}),/Сначала активируйте/);

 const confirmed=await oldDevice.request('/api/transfer/confirm',{method:'POST',authorization:oldAuth,body:{receiptCode:imported.receiptCode}});
 assert.equal(oldDevice.info().host.status,'retired');
 assert.ok(confirmed.activationToken);
 const activated=await newDevice.request('/api/transfer/activate',{method:'POST',authorization:newAuth,body:{activationToken:confirmed.activationToken}});
 assert.equal(activated.state.host.status,'active');
 await newDevice.request('/api/command',{method:'POST',authorization:newAuth,body:{id:'movement-command-003',type:'out',itemId:'product-transfer-01',from:'main-place',qty:1000}});
 await assert.rejects(()=>oldDevice.request('/api/command',{method:'POST',authorization:oldAuth,body:{id:'movement-command-004',type:'out',itemId:'product-transfer-01',from:'main-place',qty:1000}}),/больше не является главным/);
});
