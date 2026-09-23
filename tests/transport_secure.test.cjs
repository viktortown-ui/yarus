'use strict';
const test=require('node:test');
const assert=require('node:assert/strict');

global.YarusDomain=require('../web/domain.js');
global.YarusTransport=require('../web/transport.js');
let stored='',responseResolve;
global.AndroidFiles={
 loadHostState:()=>stored,
 saveHostState:value=>(stored=value,true),
 hostStateSize:()=>Buffer.byteLength(stored),
 startHostServer:()=>JSON.stringify(['http://192.168.1.20:8787']),
 stopHostServer:()=>{},
 hostRespond:(id,status,body)=>responseResolve({id,status,body})
};
require('../web/host.js');
const H=global.YarusAndroidHost,T=global.YarusTransport;

function native(request){return new Promise(resolve=>{responseResolve=resolve;H.handleNativeRequest('request-1',JSON.stringify(request));});}

test('Android LAN gateway rejects plain credentials and accepts encrypted traffic',async()=>{
 const created=await H.create({space:'Защищённый склад',device:'Планшет',login:'owner',name:'Владелец',password:'very-secure-pass'});
 const plain=await native({method:'POST',path:'/api/login',body:JSON.stringify({login:'owner',password:'very-secure-pass'}),remote:'192.168.1.44'});
 assert.equal(plain.status,426);
 const request=await T.encrypt(created.transportKey,{method:'POST',path:'/api/login',token:'',body:{login:'owner',password:'very-secure-pass'},at:Date.now()},'request');
 assert.equal(JSON.stringify(request).includes('very-secure-pass'),false);
 const secured=await native({method:'POST',path:'/api/secure',body:JSON.stringify(request),remote:'192.168.1.44'});
 assert.equal(secured.status,200);
 const opened=await T.decrypt(created.transportKey,JSON.parse(secured.body),'response',request.id);
 assert.equal(opened.status,200);assert.ok(opened.body.token);assert.equal(secured.body.includes(opened.body.token),false);
 const replay=await native({method:'POST',path:'/api/secure',body:JSON.stringify(request),remote:'192.168.1.44'});
 assert.equal(replay.status,409);
 const tampered={...request,ciphertext:request.ciphertext.slice(0,-1)+(request.ciphertext.endsWith('A')?'B':'A')};
 const rejected=await native({method:'POST',path:'/api/secure',body:JSON.stringify(tampered),remote:'192.168.1.44'});
 assert.equal(rejected.status,403);
});
