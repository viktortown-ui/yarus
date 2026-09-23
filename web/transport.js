/* Application-layer encryption for YARUS traffic over a trusted local HTTP network. */
(function(root){
 'use strict';
 const enc=new TextEncoder(),dec=new TextDecoder(),FORMAT='yarus-secure-transport',VERSION=1;
 const b64url=bytes=>{let text='',view=new Uint8Array(bytes);for(let i=0;i<view.length;i+=0x8000)text+=String.fromCharCode(...view.subarray(i,i+0x8000));return btoa(text).replace(/\+/g,'-').replace(/\//g,'_').replace(/=+$/,'');};
 const from64=value=>{const normalized=String(value||'').replace(/-/g,'+').replace(/_/g,'/'),raw=atob(normalized+'==='.slice((normalized.length+3)%4)),out=new Uint8Array(raw.length);for(let i=0;i<raw.length;i++)out[i]=raw.charCodeAt(i);return out;};
 function random(size){const value=new Uint8Array(size);crypto.getRandomValues(value);return value;}
 function createKey(){return b64url(random(32));}
 function validKey(value){try{return /^[A-Za-z0-9_-]{43}$/.test(String(value||''))&&from64(value).length===32;}catch{return false;}}
 function aad(id,direction){return enc.encode(`YARUS-TRANSPORT|${VERSION}|${id}|${direction}`);}
 async function cryptoKey(value,usage){if(!validKey(value))throw new Error('Нужен код безопасного подключения из нового приглашения.');return crypto.subtle.importKey('raw',from64(value),'AES-GCM',false,[usage]);}
 async function encrypt(keyValue,payload,direction='request',requestId=''){
  const id=requestId||b64url(random(12)),iv=random(12),key=await cryptoKey(keyValue,'encrypt'),plain=enc.encode(JSON.stringify(payload));
  const ciphertext=await crypto.subtle.encrypt({name:'AES-GCM',iv,additionalData:aad(id,direction),tagLength:128},key,plain);
  return {format:FORMAT,version:VERSION,id,iv:b64url(iv),ciphertext:b64url(ciphertext)};
 }
 async function decrypt(keyValue,envelope,direction='response',requestId=''){
  if(!envelope||envelope.format!==FORMAT||envelope.version!==VERSION||!/^[A-Za-z0-9_-]{16,80}$/.test(envelope.id||'')||requestId&&envelope.id!==requestId)throw new Error('Защищённый ответ сервера не распознан.');
  try{const iv=from64(envelope.iv),ciphertext=from64(envelope.ciphertext);if(iv.length!==12||ciphertext.length<17)throw new Error();const key=await cryptoKey(keyValue,'decrypt'),plain=await crypto.subtle.decrypt({name:'AES-GCM',iv,additionalData:aad(envelope.id,direction),tagLength:128},key,ciphertext);return JSON.parse(dec.decode(plain));}
  catch(error){if(error?.message&&error.message.includes('код безопасного'))throw error;throw new Error('Не удалось проверить защищённый ответ. Создайте новое приглашение на главном устройстве.');}
 }
 const api={FORMAT,VERSION,createKey,validKey,encrypt,decrypt};
 root.YarusTransport=api;if(typeof module!=='undefined'&&module.exports)module.exports=api;
})(typeof window!=='undefined'?window:globalThis);
