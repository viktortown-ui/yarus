/* Password-encrypted portable YARUS packages. No key or recovery code is stored in the file. */
(function(root){
 'use strict';
 const ITERATIONS=310000,enc=new TextEncoder(),dec=new TextDecoder(),ALPHABET='23456789ABCDEFGHJKLMNPQRSTUVWXYZ';
 const bytesToBase64=bytes=>{let text='',view=new Uint8Array(bytes);for(let i=0;i<view.length;i+=0x8000)text+=String.fromCharCode(...view.subarray(i,i+0x8000));return btoa(text);};
 const base64ToBytes=value=>{const raw=atob(value),out=new Uint8Array(raw.length);for(let i=0;i<raw.length;i++)out[i]=raw.charCodeAt(i);return out;};
 function random(length){const value=new Uint8Array(length);crypto.getRandomValues(value);return value;}
 function code(){const value=random(20),chars=[...value].map(x=>ALPHABET[x%ALPHABET.length]).join('');return chars.match(/.{4}/g).join('-');}
 function normalized(value){const result=String(value||'').toUpperCase().replace(/[\s-]+/g,'');if(result.length<16)throw new Error('Код защиты должен содержать не меньше 16 знаков. Используйте код, который создал ЯРУС.');return result;}
 async function keyFor(value,salt,uses){
  const material=await crypto.subtle.importKey('raw',enc.encode(normalized(value)),'PBKDF2',false,['deriveKey']);
  return crypto.subtle.deriveKey({name:'PBKDF2',salt,iterations:ITERATIONS,hash:'SHA-256'},material,{name:'AES-GCM',length:256},false,uses);
 }
 function aad(purpose,warehouseId){return enc.encode('YARUS|1|'+purpose+'|'+warehouseId);}
 async function encrypt(payload,recoveryCode,purpose='host-transfer'){
  if(!payload||typeof payload!=='object')throw new Error('Нет данных для защищённого пакета.');
  const warehouseId=String(payload.state?.space?.id||payload.space?.id||'');if(!YarusDomain.validId(warehouseId))throw new Error('Не найден идентификатор склада.');
  const salt=random(16),iv=random(12),key=await keyFor(recoveryCode,salt,['encrypt']),plain=enc.encode(JSON.stringify(payload));
  const encrypted=await crypto.subtle.encrypt({name:'AES-GCM',iv,additionalData:aad(purpose,warehouseId),tagLength:128},key,plain);
  return {format:'yarus-secure-package',version:1,appVersion:'1.1.2',purpose,warehouseId,createdAt:new Date().toISOString(),kdf:{name:'PBKDF2-HMAC-SHA256',iterations:ITERATIONS,salt:bytesToBase64(salt)},cipher:{name:'AES-256-GCM',iv:bytesToBase64(iv)},ciphertext:bytesToBase64(encrypted)};
 }
 async function decrypt(packageValue,recoveryCode,expectedPurpose=''){
  const value=typeof packageValue==='string'?JSON.parse(packageValue):packageValue;
  if(!value||value.format!=='yarus-secure-package'||value.version!==1||!YarusDomain.validId(value.warehouseId)||value.kdf?.name!=='PBKDF2-HMAC-SHA256'||value.kdf?.iterations!==ITERATIONS||value.cipher?.name!=='AES-256-GCM'||typeof value.ciphertext!=='string')throw new Error('Это не защищённый пакет ЯРУС 1.1.');
  if(expectedPurpose&&value.purpose!==expectedPurpose)throw new Error('Этот файл предназначен для другой операции ЯРУС.');
  try{
   const salt=base64ToBytes(value.kdf.salt),iv=base64ToBytes(value.cipher.iv);if(salt.length!==16||iv.length!==12)throw new Error();
   const key=await keyFor(recoveryCode,salt,['decrypt']),plain=await crypto.subtle.decrypt({name:'AES-GCM',iv,additionalData:aad(value.purpose,value.warehouseId),tagLength:128},key,base64ToBytes(value.ciphertext));
   return {package:value,payload:JSON.parse(dec.decode(plain))};
  }catch(error){if(error?.message&&/JSON/.test(error.message))throw new Error('Пакет расшифрован, но данные внутри повреждены.');throw new Error('Неверный код защиты или файл повреждён. Данные не изменены.');}
 }
 function byteLength(value){return enc.encode(typeof value==='string'?value:JSON.stringify(value)).length;}
 root.YarusTransfer={ITERATIONS,code,encrypt,decrypt,byteLength};
 if(typeof module!=='undefined'&&module.exports)module.exports=root.YarusTransfer;
})(typeof window!=='undefined'?window:globalThis);
