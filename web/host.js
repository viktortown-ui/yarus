/* Android-only authoritative warehouse host. Data at rest is encrypted by Android Keystore. */
(function(root){
 'use strict';
 const D=root.YarusDomain,APP_VERSION='1.1.2',DAY=86400;
 let state=null,addresses=[],running=false;
 const attempts=new Map();
 const enc=new TextEncoder();
 const copy=x=>D.copy(x);
 const now=()=>Math.floor(Date.now()/1000);
 const hex=bytes=>[...new Uint8Array(bytes)].map(x=>x.toString(16).padStart(2,'0')).join('');
 const b64url=bytes=>btoa(String.fromCharCode(...bytes)).replace(/\+/g,'-').replace(/\//g,'_').replace(/=+$/,'');
 function random(bytes=24){const value=new Uint8Array(bytes);crypto.getRandomValues(value);return b64url(value);}
 async function sha(value){return hex(await crypto.subtle.digest('SHA-256',enc.encode(value)));}
 async function passwordHash(password,salt){
  const key=await crypto.subtle.importKey('raw',enc.encode(password),'PBKDF2',false,['deriveBits']);
  return hex(await crypto.subtle.deriveBits({name:'PBKDF2',hash:'SHA-256',salt:enc.encode(salt),iterations:310000},key,256));
 }
 function error(message,status=400){const e=new Error(message);e.status=status;throw e;}
 function credentials(login,name,password){
  if(!/^[a-zA-Z0-9_.@+-]{3,80}$/.test(login)||password.length<10||password.length>256||!name.trim()||[...name].length>80)error('Логин: 3–80 латинских букв, цифр или . _ @ + -. Имя обязательно. Пароль: 10–256 символов.');
 }
 function owner(){return Object.values(state?.users||{}).find(x=>x.role==='owner'&&!x.disabled);}
 function effective(user){return D.permissionsFor(user);}
 function allowed(user,permission){return !!effective(user)[permission];}
 function requirePermission(user,permission,message){if(!allowed(user,permission))error(message||'Недостаточно прав.',403);}
 function makeHost(device,epoch=1){return {epoch,status:'active',device:device.trim(),updatedAt:new Date().toISOString()};}
 async function makeUser(login,name,password,role){
  const salt=random(16);return {id:D.newId(),login:login.toLowerCase().trim(),name:name.trim(),role,permissions:{},salt,hash:await passwordHash(password,salt),disabled:false};
 }
 async function sessionFor(user){
  const token=random(32),key=await sha(token);state.sessions[key]={hash:key,user:user.id,expires:now()+30*DAY};return token;
 }
 function cleanExpired(){
  const current=now();for(const [key,value] of Object.entries(state.sessions||{}))if(value.expires<current)delete state.sessions[key];
  for(const [key,value] of Object.entries(state.invites||{}))if(value.expires+DAY<current)delete state.invites[key];
 }
 async function userFrom(authorization){
  const token=String(authorization||'').replace(/^Bearer\s+/i,'');if(!token)error('Войдите в общий склад заново.',401);
  const session=state.sessions[await sha(token)],user=session&&state.users[session.user];
  if(!session||session.expires<now()||!user||user.disabled)error('Доступ отключён или вход истёк.',401);
  return user;
 }
 function snapshot(user,all=false){
  const items=copy(state.items);
  if(!allowed(user,'view_prices'))for(const item of Object.values(items))item.price=0;
  const events=all||state.events.length<=1000?copy(state.events):copy(state.events.slice(-1000));
  return {format:'yarus-data',version:1,seq:state.seq,space:copy(state.space),host:copy(state.host),items,places:copy(state.places),stocks:copy(state.stocks),events,eventCount:state.events.length,me:{id:user.id,name:user.name,login:user.login,role:user.role,permissions:effective(user)},serverTime:new Date().toISOString(),limits:{items:5000,places:200}};
 }
 function persist(){
  cleanExpired();if(!root.AndroidFiles?.saveHostState)error('Защищённое хранилище Android недоступно.',503);
  if(!AndroidFiles.saveHostState(JSON.stringify(state)))error('Android не подтвердил сохранение базы.',507);
 }
 function validLoaded(value){
  return value&&Number.isSafeInteger(value.seq)&&value.space&&D.validId(value.space.id)&&value.host&&value.host.epoch>=1&&['active','retired'].includes(value.host.status)&&value.items&&value.places&&value.stocks&&Array.isArray(value.events)&&value.users&&value.invites&&value.sessions&&value.seen;
 }
 function load(){
  if(!root.AndroidFiles?.loadHostState)return null;
  const raw=AndroidFiles.loadHostState();if(!raw)return null;if(raw.startsWith('!ERROR:'))error(raw.slice(7),500);
  const parsed=JSON.parse(raw);if(!validLoaded(parsed))error('Защищённая база главного устройства имеет неизвестный формат.',500);state=parsed;cleanExpired();return state;
 }
 function activate(){
  if(!state||state.host.status!=='active'||!root.AndroidFiles?.startHostServer){addresses=[];running=false;return [];}
  try{addresses=JSON.parse(AndroidFiles.startHostServer()||'[]');running=true;}catch{addresses=[];running=false;}return [...addresses];
 }
 function deactivate(){if(root.AndroidFiles?.stopHostServer)AndroidFiles.stopHostServer();running=false;addresses=[];}
 async function create({space,device,login,name,password}){
  if(state)error('На этом устройстве уже есть главный склад.',409);credentials(login.toLowerCase().trim(),name,password);
  if(!space?.trim()||[...space].length>80||!device?.trim()||[...device].length>80)error('Укажите название склада и понятное имя главного устройства.');
  const user=await makeUser(login,name,password,'owner');
  state={seq:0,space:{name:space.trim(),currency:'RUB',id:D.newId()},host:makeHost(device,1),items:{},places:{'main-place':{id:'main-place',name:'Основной склад',note:'',version:1}},stocks:{},events:[],users:{[user.id]:user},invites:{},sessions:{},seen:{}};
  const token=await sessionFor(user);persist();activate();return {token,state:snapshot(user)};
 }
 function rate(remote){
  const key=remote||'unknown',cut=Date.now()-5*60*1000,list=(attempts.get(key)||[]).filter(x=>x>cut);if(list.length>=12)error('Слишком много попыток. Повторите через 5 минут.',429);list.push(Date.now());attempts.set(key,list);
 }
 function parseBody(request){try{return request.body?JSON.parse(request.body):{};}catch{error('Некорректный JSON.',400);}}
 async function account(request,path){
  rate(request.remote);const input=parseBody(request),login=String(input.login||'').toLowerCase().trim();let user;
  if(path==='/api/login'){
   user=Object.values(state.users).find(x=>x.login===login);const salt=user?.salt||'dummy-salt-for-equal-work',got=await passwordHash(String(input.password||''),salt);
   if(!user||user.disabled||got!==user.hash)error('Неверный логин или пароль.',401);
  }else{
   credentials(login,String(input.name||''),String(input.password||''));if(Object.values(state.users).some(x=>x.login===login))error('Такой логин уже занят.',409);
   const key=await sha(String(input.code||'').trim()),invite=state.invites[key];if(!invite||invite.used||invite.expires<now())error('Приглашение недействительно, использовано или истекло.',403);
   invite.used=true;user=await makeUser(login,String(input.name),String(input.password),invite.role);state.users[user.id]=user;
  }
  const token=await sessionFor(user);persist();return {token,state:snapshot(user)};
 }
 function roleAllowed(role){return ['admin','manager','operator','viewer','editor'].includes(role);}
 async function command(user,input){
  if(state.host.status==='retired')error('Это устройство больше не является главным. Подключитесь к новому главному устройству.',423);
  if(!D.validId(input.id))error('Некорректный идентификатор операции.');
  const key=user.id+':'+input.id,intent=await sha(JSON.stringify(input));
  if(state.seen[key]){if(state.seen[key].intent!==intent)error('Идентификатор уже использован для другой операции.',409);return {duplicate:true,state:snapshot(user)};}
  if(input.type==='disable'){
   requirePermission(user,'team','У вас нет права управлять участниками.');if(input.userId===user.id)error('Нельзя отключить самого себя.',403);
   const target=state.users[input.userId];if(!target||target.role==='owner')error('Владелец не может быть отключён.',404);target.disabled=true;
  }else if(input.type==='user-role'){
   requirePermission(user,'team','У вас нет права управлять участниками.');if(!roleAllowed(input.role))error('Недопустимый уровень доступа.');
   const target=state.users[input.userId];if(!target||target.role==='owner')error('Владельца нельзя заменить или понизить.',404);target.role=input.role;target.permissions={};
  }else{
   const next=D.execute(state,input,user);state.seq=next.seq;state.space=next.space;state.items=next.items;state.places=next.places;state.stocks=next.stocks;state.events=next.events;
  }
  state.seen[key]={intent};persist();return {state:snapshot(user)};
 }
 async function route(request){
  if(!state)load();const path=String(request.path||'').split('?')[0],method=String(request.method||'GET').toUpperCase();
  if(path==='/api/info'&&method==='GET')return {status:200,body:{version:APP_VERSION,ready:!!state,name:'ЯРУС',host:state?copy(state.host):null}};
  if(!state)error('Главный склад на этом устройстве ещё не создан.',404);
  if(method==='POST'&&(path==='/api/login'||path==='/api/join'))return {status:200,body:await account(request,path)};
  const user=await userFrom(request.authorization);
  if(path==='/api/command'&&method==='POST')return {status:200,body:await command(user,parseBody(request))};
  if(path==='/api/invite'&&method==='POST'){
   requirePermission(user,'team','У вас нет права приглашать участников.');const input=parseBody(request);if(!roleAllowed(input.role))error('Недопустимая роль.');
   const code=random(18),key=await sha(code),invite={hash:key,role:input.role,expires:now()+DAY,used:false};state.invites[key]=invite;persist();return {status:200,body:{code,expires:invite.expires}};
  }
  if(path==='/api/logout'&&method==='POST'){
   const token=String(request.authorization||'').replace(/^Bearer\s+/i,''),key=await sha(token);delete state.sessions[key];persist();return {status:200,body:{ok:true}};
  }
  if(path==='/api/backup'&&method==='POST'){
   requirePermission(user,'backup','У вас нет права создавать копию.');persist();return {status:200,body:{filename:'защищённая внутренняя копия Android'}};
  }
  if(method==='GET'&&path==='/api/state')return {status:200,body:snapshot(user)};
  if(method==='GET'&&path==='/api/export'){requirePermission(user,'full_export','У вас нет права экспортировать полную копию.');return {status:200,body:snapshot(user,true)};}
  if(method==='GET'&&path==='/api/team'){
   requirePermission(user,'team','У вас нет права управлять участниками.');const users=Object.values(state.users).map(x=>({id:x.id,name:x.name,login:x.login,role:x.role,permissions:effective(x),disabled:!!x.disabled})).sort((a,b)=>a.name.localeCompare(b.name,'ru'));
   return {status:200,body:{users,addresses:[...addresses],host:copy(state.host)}};
  }
  if(method==='GET'&&path==='/api/storage')return {status:200,body:{journalBytes:Number(root.AndroidFiles?.hostStateSize?.()||0),packageBytes:enc.encode(JSON.stringify(state)).length,items:Object.keys(state.items).length,places:Object.keys(state.places).length,events:state.events.length,host:copy(state.host)}};
  error('Маршрут не найден.',404);
 }
 async function request(path,options={}){
  const result=await route({method:options.method||(options.body?'POST':'GET'),path,authorization:options.authorization||'',body:options.body?JSON.stringify(options.body):'',remote:'127.0.0.1'});return result.body;
 }
 async function handleNativeRequest(id,requestJson){
  let status=500,body;try{const result=await route(JSON.parse(requestJson));status=result.status;body=result.body;}catch(e){status=e.status||500;body={error:e.message||'Ошибка главного устройства.'};}
  root.AndroidFiles.hostRespond(String(id),status,JSON.stringify(body));
 }
 function validateTransfer(value){
  if(!value||value.format!=='yarus-host-state'||value.version!==1||!value.state)error('Это не пакет главного устройства ЯРУС.');
  const s=value.state;if(!validLoaded(s)||s.host.status!=='active')error('Пакет главного устройства повреждён.');
  D.validateBackup({format:'yarus-data',version:1,seq:s.seq,space:s.space,items:s.items,places:s.places,stocks:s.stocks,events:s.events,eventCount:s.events.length});
  const users=Object.values(s.users),owners=users.filter(x=>x.role==='owner');if(owners.length!==1||users.some(x=>!D.validId(x.id)||!x.login||!x.name||!x.salt||!/^[0-9a-f]{64}$/.test(x.hash)))error('В пакете повреждены данные участников.');return s;
 }
 async function transferState(device){
  if(!state)error('Главный склад не найден.',404);const user=owner();if(!user)error('Владелец не найден.',500);
  if(!device?.trim()||[...device].length>80)error('Укажите понятное имя нового главного устройства.');
  if(state.host.status!=='retired'){state.host={...state.host,status:'retired',transferId:D.newId(),updatedAt:new Date().toISOString()};persist();}
  const moved=copy(state);moved.host={...makeHost(device,state.host.epoch+1),transferId:state.host.transferId};moved.invites={};moved.sessions={};
  deactivate();return {format:'yarus-host-state',version:1,appVersion:APP_VERSION,createdAt:new Date().toISOString(),state:moved};
 }
 async function importTransfer(value){
  if(state)error('На этом устройстве уже есть главный склад. Импортируйте пакет на пустом устройстве.',409);const imported=validateTransfer(value);state=copy(imported);state.invites={};state.sessions={};state.seen=state.seen||{};
  const user=owner();const token=await sessionFor(user);persist();activate();return {token,state:snapshot(user)};
 }
 function info(){return {exists:!!state,running,addresses:[...addresses],host:state?copy(state.host):null,size:Number(root.AndroidFiles?.hostStateSize?.()||0)};}
 function resumeNative(){try{if(!state)load();if(state?.host.status==='active')activate();}catch(e){if(root.toast)root.toast(e.message,'error');}}
 function nativeStopped(){running=false;addresses=[];}
 try{load();}catch(e){setTimeout(()=>root.toast?.(e.message,'error'),0);}
 root.YarusAndroidHost={load,create,activate,deactivate,resumeNative,nativeStopped,request,handleNativeRequest,transferState,importTransfer,info};
})(typeof window!=='undefined'?window:globalThis);
