'use strict';
const test=require('node:test');const assert=require('node:assert/strict');
global.YarusDomain=require('../web/domain.js');global.YarusTransport=require('../web/transport.js');let stored='';
global.AndroidFiles={loadHostState:()=>stored,saveHostState:value=>(stored=value,true),hostStateSize:()=>Buffer.byteLength(stored),startHostServer:()=>JSON.stringify(['http://192.168.1.20:8787']),stopHostServer:()=>{},hostRespond:()=>{}};
require('../web/host.js');const H=global.YarusAndroidHost;

test('Android host owns state, grants roles and transfers in two safe steps',async()=>{
 const created=await H.create({space:'Тестовый склад',device:'Тестовый планшет',login:'owner',name:'Владелец',password:'very-secure-pass'});assert.equal(created.state.me.role,'owner');assert.equal(H.info().running,true);
 const auth='Bearer '+created.token,invite=await H.request('/api/invite',{method:'POST',authorization:auth,body:{role:'operator'}});
 const joined=await H.request('/api/join',{method:'POST',body:{code:invite.code,login:'worker',name:'Кладовщик',password:'another-secure-pass'}});assert.equal(joined.state.me.role,'operator');
 const worker='Bearer '+joined.token;await assert.rejects(()=>H.request('/api/command',{method:'POST',authorization:worker,body:{id:'command-0001',type:'item',item:{id:'product-0001',name:'Товар',sku:'T-1',barcode:'',category:'',unit:'шт',min:0,price:0,note:'',fields:{},archived:false,version:0}}}),/нет права/);
 const command={id:'command-0002',type:'item',item:{id:'product-0001',name:'Товар',sku:'T-1',barcode:'',category:'',unit:'шт',min:0,price:0,note:'',fields:{},archived:false,version:0}};await H.request('/api/command',{method:'POST',authorization:auth,body:command});
 const movement={id:'command-0003',type:'in',itemId:'product-0001',to:'main-place',qty:1000};const result=await H.request('/api/command',{method:'POST',authorization:worker,body:movement});assert.equal(result.state.stocks['product-0001@main-place'].qty,1000);
 const duplicate=await H.request('/api/command',{method:'POST',authorization:worker,body:movement});assert.equal(duplicate.duplicate,true);
 let transfer=await H.transferState('Новый компьютер');assert.equal(transfer.version,2);assert.equal(transfer.state.host.status,'awaiting_activation');assert.equal(transfer.state.host.epoch,2);assert.equal(H.info().host.status,'transfer_pending');assert.equal(transfer.state.transfer.activationToken,undefined);
 await assert.rejects(()=>H.request('/api/command',{method:'POST',authorization:auth,body:{...movement,id:'command-0004'}}),/Перенос ещё не завершён/);
 await H.request('/api/transfer/cancel',{method:'POST',authorization:auth,body:{}});assert.equal(H.info().host.status,'active');
 await H.request('/api/command',{method:'POST',authorization:auth,body:{...movement,id:'command-0005'}});
 transfer=await H.transferState('Новый планшет');
 await assert.rejects(()=>H.request('/api/transfer/confirm',{method:'POST',authorization:auth,body:{receiptCode:'wrong-code'}}),/не подходит/);
 const confirmed=await H.request('/api/transfer/confirm',{method:'POST',authorization:auth,body:{receiptCode:transfer.state.transfer.receiptCode}});assert.ok(confirmed.activationToken);assert.equal(H.info().host.status,'retired');
 await assert.rejects(()=>H.request('/api/command',{method:'POST',authorization:auth,body:{...movement,id:'command-0006'}}),/больше не является главным/);
});
