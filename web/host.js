/* Android-only authoritative warehouse host. Data at rest is encrypted by Android Keystore. */
(function(root){
 'use strict';
 const D=root.YarusDomain,APP_VERSION='1.2.0',DAY=86400,RECENT_COMMAND_LIMIT=D.RECENT_COMMAND_LIMIT||10000,ANDROID_WARN_BYTES=160*1024*1024,ANDROID_STOP_BYTES=220*1024*1024;
 let state=null,addresses=[],running=false;
 const attempts=new Map();
 const secureIDs=new Map();
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
 async function sessionFor(user,target=state){
  const token=random(32),key=await sha(token);target.sessions[key]={hash:key,user:user.id,expires:now()+30*DAY};return token;
 }
 function cleanExpired(target=state){
  const current=now();for(const [key,value] of Object.entries(target.sessions||{}))if(value.expires<current)delete target.sessions[key];
  for(const [key,value] of Object.entries(target.invites||{}))if(value.expires+DAY<current)delete target.invites[key];
 }
 function pruneRecent(target){
  let changed=false;target.seen=target.seen||{};const seen=Object.keys(target.seen).sort((left,right)=>(target.seen[left]?.seq||0)-(target.seen[right]?.seq||0)||left.localeCompare(right));
  for(let index=0;index<seen.length-RECENT_COMMAND_LIMIT;index++){delete target.seen[seen[index]];changed=true;}
  if(target.localSeen){const local=Object.keys(target.localSeen);for(let index=0;index<local.length-RECENT_COMMAND_LIMIT;index++){delete target.localSeen[local[index]];changed=true;}}
  return changed;
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
  const events=all?copy(state.events):copy(D.recentEvents(state,1000));
  return {format:'yarus-data',version:2,seq:state.seq,space:copy(state.space),host:copy(state.host),items,places:copy(state.places),stocks:copy(state.stocks),historySegments:all?copy(state.historySegments||[]):null,events,eventCount:D.historyCount(state),me:{id:user.id,name:user.name,login:user.login,role:user.role,permissions:effective(user)},serverTime:new Date().toISOString(),limits:{items:5000,places:200}};
 }
 function persist(next=state){
  cleanExpired(next);if(!root.AndroidFiles?.saveHostState)error('Защищённое хранилище Android недоступно.',503);
  if(!AndroidFiles.saveHostState(JSON.stringify(next)))error('Android не подтвердил сохранение базы.',507);state=next;
 }
 function validLoaded(value){
  return value&&Number.isSafeInteger(value.seq)&&value.space&&D.validId(value.space.id)&&value.host&&value.host.epoch>=1&&['active','transfer_pending','awaiting_activation','retired'].includes(value.host.status)&&value.items&&value.places&&value.stocks&&Array.isArray(value.events)&&value.users&&value.invites&&value.sessions&&value.seen;
 }
 function load(){
  if(!root.AndroidFiles?.loadHostState)return null;
  const raw=AndroidFiles.loadHostState();if(!raw)return null;if(raw.startsWith('!ERROR:'))error(raw.slice(7),500);
  const parsed=JSON.parse(raw);if(!validLoaded(parsed))error('Защищённая база главного устройства имеет неизвестный формат.',500);state=D.normalizeHistory(parsed);let changed=pruneRecent(state);if(!root.YarusTransport?.validKey(state.transportKey)){state.transportKey=YarusTransport.createKey();changed=true;}if(changed)persist(state);else cleanExpired();return state;
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
  const next={seq:0,space:{name:space.trim(),currency:'RUB',id:D.newId()},host:makeHost(device,1),items:{},places:{'main-place':{id:'main-place',name:'Основной склад',note:'',version:1}},stocks:{},historySegments:[],events:[],eventCount:0,users:{[user.id]:user},invites:{},sessions:{},seen:{},transportKey:YarusTransport.createKey()};
  const token=await sessionFor(user,next);persist(next);activate();return {token,transportKey:state.transportKey,state:snapshot(user)};
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
   const next={...state,sessions:{...state.sessions}},token=await sessionFor(user,next);persist(next);return {token,transportKey:state.transportKey,state:snapshot(user)};
  }else{
   credentials(login,String(input.name||''),String(input.password||''));if(Object.values(state.users).some(x=>x.login===login))error('Такой логин уже занят.',409);
   const key=await sha(String(input.code||'').trim()),invite=state.invites[key];if(!invite||invite.used||invite.expires<now())error('Приглашение недействительно, использовано или истекло.',403);
   user=await makeUser(login,String(input.name),String(input.password),invite.role);const next={...state,invites:{...state.invites,[key]:{...invite,used:true}},users:{...state.users,[user.id]:user},sessions:{...state.sessions}},token=await sessionFor(user,next);persist(next);return {token,transportKey:state.transportKey,state:snapshot(user)};
  }
 }
 function roleAllowed(role){return ['admin','manager','operator','viewer','editor'].includes(role);}
 function isLoopback(remote){const value=String(remote||'');return value==='::1'||value==='localhost'||/^127\./.test(value)||/^\[::1\](?::\d+)?$/.test(value);}
 async function secureRoute(request){
  const envelope=parseBody(request);let opened;try{opened=await YarusTransport.decrypt(state.transportKey,envelope,'request',envelope.id);}catch{error('Защищённый запрос не прошёл проверку. Создайте новое приглашение.',403);}const method=String(opened.method||'').toUpperCase(),path=String(opened.path||'');
  if(!['GET','POST'].includes(method)||!path.startsWith('/api/')||path==='/api/secure'||path.length>160||Math.abs(Date.now()-Number(opened.at))>300000)error('Защищённый запрос повреждён или устарел.',400);
  const current=Date.now();for(const [id,seenAt] of secureIDs)if(current-seenAt>600000)secureIDs.delete(id);if(secureIDs.has(envelope.id))error('Защищённый запрос уже был обработан. Повторите действие из приложения.',409);while(secureIDs.size>=20000)secureIDs.delete(secureIDs.keys().next().value);secureIDs.set(envelope.id,current);
  let result;try{result=await route({method,path,authorization:opened.token?'Bearer '+opened.token:'',body:method==='POST'?JSON.stringify(opened.body||{}):'',remote:request.remote,secure:true});}
  catch(failure){result={status:failure.status||500,body:{error:failure.message||'Ошибка главного устройства.'}};}
  return {status:200,body:await YarusTransport.encrypt(state.transportKey,{status:result.status,body:result.body},'response',envelope.id)};
 }
 function ensureWriteCapacity(){const bytes=Number(root.AndroidFiles?.hostStateSize?.()||0);if(bytes>=ANDROID_STOP_BYTES)error('База на телефоне или планшете достигла безопасного размера. Сохраните полную копию и перенесите главное устройство на компьютер; до переноса новые изменения остановлены.',507);}
 async function command(user,input){
  if(state.host.status!=='active'){
   if(state.host.status==='transfer_pending')error('Перенос ещё не завершён. Отмените его или подтвердите новое устройство.',423);
   if(state.host.status==='awaiting_activation')error('Сначала активируйте это новое главное устройство.',423);
   error('Это устройство больше не является главным. Подключитесь к новому главному устройству.',423);
  }
  if(!D.validId(input.id))error('Некорректный идентификатор операции.');
  const key=user.id+':'+input.id,intent=await sha(JSON.stringify(input));
  if(state.seen[key]){if(state.seen[key].intent!==intent)error('Идентификатор уже использован для другой операции.',409);return {duplicate:true,state:snapshot(user)};}
  let next=state;
  if(input.type==='disable'){
   requirePermission(user,'team','У вас нет права управлять участниками.');if(input.userId===user.id)error('Нельзя отключить самого себя.',403);
   const target=state.users[input.userId];if(!target||target.role==='owner')error('Владелец не может быть отключён.',404);next={...state,users:{...state.users,[target.id]:{...target,disabled:true}}};
  }else if(input.type==='user-role'){
   requirePermission(user,'team','У вас нет права управлять участниками.');if(!roleAllowed(input.role))error('Недопустимый уровень доступа.');
   const target=state.users[input.userId];if(!target||target.role==='owner')error('Владельца нельзя заменить или понизить.',404);next={...state,users:{...state.users,[target.id]:{...target,role:input.role,permissions:{}}}};
  }else{
   ensureWriteCapacity();
   next=D.execute(state,input,user);
  }
  next={...next,seen:{...next.seen,[key]:{intent,seq:next.seq}}};pruneRecent(next);persist(next);return {state:snapshot(user)};
 }
 async function route(request){
  if(!state)load();const path=String(request.path||'').split('?')[0],method=String(request.method||'GET').toUpperCase();
  if(path==='/api/info'&&method==='GET')return {status:200,body:{version:APP_VERSION,ready:!!state,name:'ЯРУС',host:state?copy(state.host):null}};
  if(!state)error('Главный склад на этом устройстве ещё не создан.',404);
  if(path==='/api/secure'&&method==='POST')return secureRoute(request);
  if(!isLoopback(request.remote)&&!request.secure)error('Для HTTP нужен код безопасного подключения из нового QR-приглашения.',426);
  if(method==='POST'&&(path==='/api/login'||path==='/api/join'))return {status:200,body:await account(request,path)};
  const user=await userFrom(request.authorization);
  if(path.startsWith('/api/transfer/')&&!isLoopback(request.remote))error('Перенос главного устройства подтверждается только на самом устройстве.',403);
  if(path==='/api/transfer/prepare'&&method==='POST'){
   requirePermission(user,'host_transfer','Только владелец передаёт роль главного устройства.');return {status:200,body:{transfer:await transferState(String(parseBody(request).device||''))}};
  }
  if(path==='/api/transfer/status'&&method==='GET'){
   requirePermission(user,'host_transfer','Только владелец управляет переносом.');return {status:200,body:{host:copy(state.host),...(state.transfer?{transfer:copy(state.transfer)}:{})}};
  }
  if(path==='/api/transfer/cancel'&&method==='POST'){
   requirePermission(user,'host_transfer','Только владелец управляет переносом.');if(state.host.status!=='transfer_pending'||!state.transfer)error('Этот перенос уже нельзя отменить.',409);
   const next={...state,host:{...state.host,status:'active',transferId:undefined,updatedAt:new Date().toISOString()}};delete next.transfer;persist(next);activate();return {status:200,body:{ok:true,host:copy(state.host)}};
  }
  if(path==='/api/transfer/confirm'&&method==='POST'){
   requirePermission(user,'host_transfer','Только владелец управляет переносом.');const receipt=String(parseBody(request).receiptCode||'').trim();
   if(state.host.status!=='transfer_pending'||!state.transfer||receipt!==state.transfer.receiptCode)error('Код подтверждения не подходит. Проверьте код на новом устройстве.',403);
   const next={...state,host:{...state.host,status:'retired',updatedAt:new Date().toISOString()},transfer:{...state.transfer}};next.transfer.confirmedAt=next.host.updatedAt;persist(next);deactivate();return {status:200,body:{activationToken:state.transfer.activationToken,host:copy(state.host)}};
  }
  if(path==='/api/transfer/activate'&&method==='POST'){
   requirePermission(user,'host_transfer','Только владелец управляет переносом.');const token=String(parseBody(request).activationToken||'').trim();
   if(state.host.status!=='awaiting_activation'||!state.transfer||await sha(token)!==state.transfer.activationHash)error('Код активации не подходит. Возьмите его на старом главном устройстве.',403);
   const next={...state,host:{...state.host,status:'active',updatedAt:new Date().toISOString()}};delete next.transfer;persist(next);activate();return {status:200,body:{ok:true,state:snapshot(user)}};
  }
  if(path==='/api/command'&&method==='POST')return {status:200,body:await command(user,parseBody(request))};
  if(path==='/api/invite'&&method==='POST'){
   requirePermission(user,'team','У вас нет права приглашать участников.');const input=parseBody(request);if(!roleAllowed(input.role))error('Недопустимая роль.');
   const code=random(18),key=await sha(code),invite={hash:key,role:input.role,expires:now()+DAY,used:false},next={...state,invites:{...state.invites,[key]:invite}};persist(next);return {status:200,body:{code,expires:invite.expires,transportKey:state.transportKey}};
  }
  if(path==='/api/logout'&&method==='POST'){
   const token=String(request.authorization||'').replace(/^Bearer\s+/i,''),key=await sha(token),next={...state,sessions:{...state.sessions}};delete next.sessions[key];persist(next);return {status:200,body:{ok:true}};
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
  if(method==='GET'&&path==='/api/storage')return {status:200,body:{journalBytes:Number(root.AndroidFiles?.hostStateSize?.()||0),backupBytes:Number(root.AndroidFiles?.hostBackupSize?.()||0),backupCount:Number(root.AndroidFiles?.hostBackupCount?.()||0),packageBytes:enc.encode(JSON.stringify(state)).length,items:Object.keys(state.items).length,places:Object.keys(state.places).length,events:D.historyCount(state),warningBytes:ANDROID_WARN_BYTES,stopBytes:ANDROID_STOP_BYTES,host:copy(state.host)}};
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
  if(!value||value.format!=='yarus-host-state'||![1,2].includes(value.version)||!value.state)error('Это не пакет главного устройства ЯРУС.');
  const s=value.state;if(!validLoaded(s))error('Пакет главного устройства повреждён.');
  if(value.version===1){if(s.host.status!=='active')error('Старый пакет переноса содержит неверный статус устройства.');delete s.transfer;}
  else if(s.host.status!=='awaiting_activation'||!s.transfer||s.transfer.id!==s.host.transferId||!s.transfer.receiptCode||!s.transfer.activationHash||s.transfer.activationToken)error('Новый пакет переноса повреждён или уже использован.');
  D.validateBackup({format:'yarus-data',version:value.version===1?1:2,seq:s.seq,space:s.space,items:s.items,places:s.places,stocks:s.stocks,historySegments:s.historySegments||[],events:s.events,eventCount:value.version===1?s.events.length:s.eventCount});
  const users=Object.values(s.users),owners=users.filter(x=>x.role==='owner');if(owners.length!==1||users.some(x=>!D.validId(x.id)||!x.login||!x.name||!x.salt||!/^[0-9a-f]{64}$/.test(x.hash)))error('В пакете повреждены данные участников.');return s;
 }
 async function transferState(device){
  if(!state)error('Главный склад не найден.',404);const user=owner();if(!user)error('Владелец не найден.',500);
  if(!device?.trim()||[...device].length>80)error('Укажите понятное имя нового главного устройства.');
  if(state.host.status==='active'){
   const activationToken=random(24),startedAt=new Date().toISOString(),transfer={id:D.newId(),targetDevice:device.trim(),receiptCode:random(15),activationToken,activationHash:await sha(activationToken),startedAt};
   const next={...state,transfer,host:{...state.host,status:'transfer_pending',transferId:transfer.id,updatedAt:startedAt}};persist(next);deactivate();
  }else if(state.host.status!=='transfer_pending'||!state.transfer)error(state.host.status==='retired'?'Роль главного устройства уже передана.':'Нельзя подготовить перенос в текущем состоянии.',409);
  const moved=copy(state),pending=state.transfer;moved.host={...makeHost(pending.targetDevice,state.host.epoch+1),status:'awaiting_activation',transferId:pending.id};
  moved.transfer={id:pending.id,targetDevice:pending.targetDevice,receiptCode:pending.receiptCode,activationHash:pending.activationHash,startedAt:pending.startedAt};moved.invites={};moved.sessions={};
  return {format:'yarus-host-state',version:2,appVersion:APP_VERSION,createdAt:new Date().toISOString(),state:moved};
 }
 async function importTransfer(value){
  if(state&&state.host?.status!=='awaiting_activation')error('На этом устройстве уже есть главный склад. Импортируйте пакет на пустом устройстве.',409);const imported=validateTransfer(value),next=D.normalizeHistory(copy(imported));next.invites={};next.sessions={};next.seen=next.seen||{};pruneRecent(next);if(!YarusTransport.validKey(next.transportKey))next.transportKey=YarusTransport.createKey();
  const user=Object.values(next.users||{}).find(x=>x.role==='owner'&&!x.disabled);if(!user)error('В пакете не найден владелец.',400);const token=await sessionFor(user,next),receiptCode=next.transfer?.receiptCode;persist(next);if(state.host.status==='active')activate();return {token,transportKey:state.transportKey,state:snapshot(user),...(receiptCode?{receiptCode}:{})};
 }
 function info(){return {exists:!!state,running,addresses:[...addresses],host:state?copy(state.host):null,size:Number(root.AndroidFiles?.hostStateSize?.()||0)};}
 function resumeNative(){try{if(!state)load();if(state?.host.status==='active')activate();}catch(e){if(root.toast)root.toast(e.message,'error');}}
 function nativeStopped(){running=false;addresses=[];}
 try{load();}catch(e){setTimeout(()=>root.toast?.(e.message,'error'),0);}
 root.YarusAndroidHost={load,create,activate,deactivate,resumeNative,nativeStopped,request,handleNativeRequest,transferState,importTransfer,info};
})(typeof window!=='undefined'?window:globalThis);
