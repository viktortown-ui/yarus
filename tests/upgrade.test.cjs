'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),fs=require('fs'),vm=require('vm'),path=require('path');
const ctx={crypto:require('node:crypto').webcrypto};ctx.globalThis=ctx;vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../web/domain.js'),'utf8'),ctx);const D=ctx.YarusDomain;
const legacy=JSON.parse(fs.readFileSync(path.join(__dirname,'fixtures/legacy-v091.json')));
test('0.9.1 real-format backup restored with stock, history, units, factory code and fields unchanged',()=>{
 assert.equal(legacy.space.id,undefined);const restored=D.validateBackup(legacy);
 assert.equal(D.total(restored,'legacy-canister'),17000);
 for(const field of ['items','places','stocks','events'])assert.equal(JSON.stringify(restored[field]),JSON.stringify(legacy[field]),field);
 assert.equal(restored.space.name,'Хемиком');assert.match(restored.space.id,/^[A-Za-z0-9_-]{8,80}$/);
 assert.equal(legacy.space.id,undefined,'validation must not mutate input');
});
test('0.10 full backup preserves namespace and its existing labels',()=>{const s=D.validateBackup(legacy);const s2=D.validateBackup(JSON.parse(JSON.stringify(s)));assert.equal(s2.space.id,s.space.id);assert.equal(D.total(s2,'legacy-canister'),17000);});
test('renaming local warehouse cannot change QR namespace',()=>{const s=D.validateBackup(legacy);const s2=D.execute(s,{id:'rename-namespace',type:'space',space:{name:'Переименован',currency:'EUR',id:'forged-namespace'}});assert.equal(s2.space.id,s.space.id);assert.equal(D.total(s2,'legacy-canister'),17000);});
test('invalid namespace backup rejected without mutating input',()=>{const s=D.validateBackup(legacy);s.space.id='__proto__';assert.throws(()=>D.validateBackup(s));});
