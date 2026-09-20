'use strict';
const test=require('node:test');const assert=require('node:assert/strict');
global.YarusDomain=require('../web/domain.js');const T=require('../web/transfer.js');

test('encrypted host package round-trips and rejects a wrong code or tampering',async()=>{
 const recovery=T.code();assert.match(recovery,/^(?:[23456789A-HJ-NP-Z]{4}-){4}[23456789A-HJ-NP-Z]{4}$/);
 const payload={format:'yarus-host-state',version:1,state:{space:{id:'warehouse-0001'},secret:'пароли-и-история'}};
 const secured=await T.encrypt(payload,recovery,'host-transfer');const serialized=JSON.stringify(secured);
 assert.equal(serialized.includes('пароли-и-история'),false);assert.equal(secured.kdf.iterations,310000);assert.equal(secured.cipher.name,'AES-256-GCM');
 const opened=await T.decrypt(serialized,recovery,'host-transfer');assert.deepEqual(opened.payload,payload);
 await assert.rejects(()=>T.decrypt(serialized,'2222-2222-2222-2222-2222','host-transfer'),/Неверный код/);
 const changed={...secured,ciphertext:secured.ciphertext.slice(0,-2)+'AA'};await assert.rejects(()=>T.decrypt(changed,recovery,'host-transfer'),/повреждён/);
});
