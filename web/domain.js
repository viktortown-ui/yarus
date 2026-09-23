/* Shared, dependency-free inventory rules. Quantities are integer thousandths. */
(function(root){
 'use strict';
 const MAX_QTY=1e12,MAX_PRICE=10_000_000_000_000,HISTORY_SEGMENT_SIZE=5000,RECENT_COMMAND_LIMIT=10000,UNITS=['шт','кг','г','л','мл','м','м²','м³','упак','компл','т'];
 const PERMISSIONS=['catalog','places','stock','reverse','settings','team','full_export','backup','view_prices','host_transfer'];
 const ROLE_PERMISSIONS={
  owner:[...PERMISSIONS],
  admin:PERMISSIONS.filter(x=>x!=='host_transfer'),
  manager:['catalog','places','stock','view_prices'],
  editor:['catalog','places','stock','view_prices'],
  operator:['stock'],
  viewer:[]
 };
 const copy=x=>JSON.parse(JSON.stringify(x));
 const validId=x=>typeof x==='string'&&/^[A-Za-z0-9_-]{8,80}$/.test(x)&&!['__proto__','constructor','prototype'].includes(x);
 const fail=s=>{throw new Error(s)};
 const text=(s,n)=>typeof s==='string'&&[...s].length<=n&&!s.includes('\0');
 function decimal(value,digits=3,max=MAX_QTY){
  const s=String(value).trim().replace(',','.');const re=new RegExp('^\\d+(?:\\.\\d{1,'+digits+'})?$');
  if(!re.test(s))fail('Введите неотрицательное число, не более '+digits+' знаков после запятой.');
  const [whole,fraction='']=s.split('.');const n=Number(whole)*10**digits+Number(fraction.padEnd(digits,'0'));
  if(!Number.isSafeInteger(n)||n>max)fail('Число слишком большое.');return n;
 }
 function createState(name='Мой склад',demo=false){return {format:'yarus-data',version:2,seq:0,space:{name,currency:'RUB',id:newId()},items:{},places:{'main-place':{id:'main-place',name:'Основной склад',note:'',version:1}},stocks:{},historySegments:[],events:[],eventCount:0,me:{id:'local-owner',name:'Вы',login:'local',role:'owner'},demo};}
 function newId(){const bytes=new Uint8Array(16);globalThis.crypto.getRandomValues(bytes);return [...bytes].map(v=>v.toString(16).padStart(2,'0')).join('');}
 function permissionsFor(actor){
  const granted=new Set(ROLE_PERMISSIONS[actor?.role]||[]);
  if(actor?.permissions&&typeof actor.permissions==='object')for(const [name,value] of Object.entries(actor.permissions)){
   if(!PERMISSIONS.includes(name))continue;if(value)granted.add(name);else granted.delete(name);
  }
  return Object.fromEntries(PERMISSIONS.map(name=>[name,granted.has(name)]));
 }
 function can(actor,permission){return !!permissionsFor(actor)[permission];}
 function requirePermission(actor,permission,message){if(!can(actor,permission))fail(message||'Недостаточно прав для этого действия.');}
 const key=(item,place)=>item+'@'+place;
 function total(s,id,place=''){return Object.values(s.stocks).filter(x=>x.item===id&&(!place||x.place===place)).reduce((v,x)=>v+x.qty,0);}
 function historyCount(s){return (Array.isArray(s.historySegments)?s.historySegments.reduce((n,segment)=>n+(Array.isArray(segment)?segment.length:0),0):0)+(Array.isArray(s.events)?s.events.length:0);}
 function eachEvent(s,visit){for(const segment of (s.historySegments||[]))for(const event of segment)if(visit(event)===false)return false;for(const event of (s.events||[]))if(visit(event)===false)return false;return true;}
 function historySome(s,predicate){let found=false;eachEvent(s,event=>{if(predicate(event)){found=true;return false;}});return found;}
 function findEvent(s,predicate){let found;eachEvent(s,event=>{if(predicate(event)){found=event;return false;}});return found;}
 function countEvents(s,predicate){let count=0;eachEvent(s,event=>{if(predicate(event))count++;});return count;}
 function recentEvents(s,limit=1000){const result=[],current=Array.isArray(s.events)?s.events:[];for(let i=current.length-1;i>=0&&result.length<limit;i--)result.push(current[i]);const segments=Array.isArray(s.historySegments)?s.historySegments:[];for(let n=segments.length-1;n>=0&&result.length<limit;n--)for(let i=segments[n].length-1;i>=0&&result.length<limit;i--)result.push(segments[n][i]);return result.reverse();}
 function allEvents(s){return [...(s.historySegments||[]).flat(),...(s.events||[])];}
 function normalizeHistory(s){
  s.historySegments=Array.isArray(s.historySegments)?s.historySegments:[];s.events=Array.isArray(s.events)?s.events:[];
  if(s.events.length>HISTORY_SEGMENT_SIZE){const events=s.events;s.events=[];for(let i=0;i<events.length;i+=HISTORY_SEGMENT_SIZE){const chunk=events.slice(i,i+HISTORY_SEGMENT_SIZE);if(i+HISTORY_SEGMENT_SIZE<events.length)s.historySegments.push(chunk);else s.events=chunk;}}
  s.eventCount=historyCount(s);s.version=2;return s;
 }
 function appendEvent(s,event){normalizeHistory(s);if(s.events.length>=HISTORY_SEGMENT_SIZE){s.historySegments=[...s.historySegments,s.events];s.events=[];}s.events.push(event);s.eventCount=historyCount(s);}
 function rememberRecent(target,key,value){target[key]=value;const keys=Object.keys(target);for(let index=0;index<keys.length-RECENT_COMMAND_LIMIT;index++)delete target[keys[index]];}
 function workingCopy(state){const archived=Array.isArray(state.historySegments)?state.historySegments:[],s=copy({...state,historySegments:[]});s.historySegments=archived;return normalizeHistory(s);}
 function validateItem(s,item){
  const x=copy(item);x.name=(x.name||'').trim();x.sku=(x.sku||'').trim();x.barcode=(x.barcode||'').trim();x.category=(x.category||'').trim();x.note=x.note||'';x.fields=x.fields||{};x.archived=!!x.archived;
  if(!validId(x.id)||!x.name||!text(x.name,150)||!text(x.sku,80)||!text(x.barcode,80)||!text(x.category,80)||!text(x.note,2000)||!UNITS.includes(x.unit)||!Number.isSafeInteger(x.min)||x.min<0||x.min>MAX_QTY||!Number.isSafeInteger(x.price)||x.price<0||x.price>MAX_PRICE||Object.keys(x.fields).length>12)fail('Проверьте карточку: название, единица, минимум и цена обязательны.');
  for(const [k,v] of Object.entries(x.fields))if(!k.trim()||!text(k,40)||!text(v,160))fail('Дополнительное поле: до 40 символов в названии и 160 в значении.');
  const old=s.items[x.id];if(old&&x.version!==old.version)fail('Карточка уже изменилась. Откройте её заново.');
  if(!old&&Object.keys(s.items).length>=5000)fail('Лимит этой версии — 5000 товаров.');
  if(old&&old.unit!==x.unit&&historySome(s,e=>e.item===x.id))fail('Единицу товара с историей менять нельзя. Создайте другую карточку.');
  for(const y of Object.values(s.items))if(y.id!==x.id&&((x.sku&&y.sku.toLowerCase()===x.sku.toLowerCase())||(x.barcode&&y.barcode.toLowerCase()===x.barcode.toLowerCase())))fail('Артикул или штрихкод уже есть, включая архив.');
  if(x.archived&&total(s,x.id)!==0)fail('Сначала обнулите остатки во всех местах.');
  x.version=(old?.version||0)+1;return x;
 }
 function execute(state,c,actor=state.me){
  if(!validId(c.id))fail('Некорректный идентификатор операции.');
  const s=workingCopy(state);s.localSeen=s.localSeen||{};const intent=JSON.stringify(c);
  if(s.localSeen[c.id]){if(s.localSeen[c.id]!==intent)fail('Этот номер уже использован другой операцией.');return s;}
  if(!text(c.note||'',1000)||!text(c.ref||'',100))fail('Слишком длинное примечание.');
  if(c.type==='item'){requirePermission(actor,'catalog','У вас нет права изменять каталог.');const x=validateItem(s,c.item);s.items[x.id]=x;}
  else if(c.type==='place'){
   requirePermission(actor,'places','У вас нет права изменять места хранения.');
   const x=copy(c.place);x.name=(x.name||'').trim();x.note=x.note||'';
   if(!validId(x.id)||!x.name||!text(x.name,100)||!text(x.note,200))fail('Укажите название места до 100 символов.');
   if((s.places[x.id]?.version||0)!==x.version)fail('Место уже изменилось.');
   if(!s.places[x.id]&&Object.keys(s.places).length>=200)fail('Лимит — 200 мест.');
   if(Object.values(s.places).some(p=>p.id!==x.id&&p.name.toLowerCase()===x.name.toLowerCase()))fail('Такое место уже существует.');x.version++;s.places[x.id]=x;
  }else if(c.type==='space'){
   requirePermission(actor,'settings','У вас нет права менять настройки склада.');
   if(!text(c.space?.name,80)||!c.space.name.trim()||!['RUB','EUR','USD'].includes(c.space.currency))fail('Проверьте название склада и валюту.');s.space={...copy(c.space),id:s.space.id||newId()};
  }else if(['in','out','transfer','count','reverse'].includes(c.type)){
   requirePermission(actor,c.type==='reverse'?'reverse':'stock',c.type==='reverse'?'У вас нет права отменять движения.':'У вас нет права проводить движения.');
   const e={id:c.id,kind:c.type,item:c.itemId||'',from:c.from||'',to:c.to||'',qty:c.qty||0,note:c.note||'',ref:c.ref||'',actor:actor.id,actorName:actor.name,at:new Date().toISOString(),seq:s.seq+1,changes:[]};
   if(c.type==='reverse'){
    const original=findEvent(s,x=>x.id===c.ref);
    if(!original||original.kind==='reverse')fail('Исходная операция не найдена.');
    if(historySome(s,x=>x.kind==='reverse'&&x.ref===c.ref))fail('Операция уже отменена.');
    if(!(c.note||'').trim())fail('Укажите причину отмены.');
    Object.assign(e,{item:original.item,qty:original.qty,from:original.to,to:original.from,changes:original.changes.map(d=>({...d,qty:-d.qty}))});
   }else{
    const item=s.items[c.itemId];if(!item||item.archived)fail('Товар отсутствует или в архиве.');
    if(!Number.isSafeInteger(c.qty)||c.qty<0||c.qty>MAX_QTY||(c.type!=='count'&&c.qty===0))fail('Количество должно быть больше нуля, с точностью до 0,001.');
    if(c.type==='in')e.changes=[{item:c.itemId,place:c.to,qty:c.qty}];
    if(c.type==='out')e.changes=[{item:c.itemId,place:c.from,qty:-c.qty}];
    if(c.type==='transfer'){
     if(c.from===c.to)fail('Выберите разные места.');
     e.changes=[{item:c.itemId,place:c.from,qty:-c.qty},{item:c.itemId,place:c.to,qty:c.qty}];
    }
    if(c.type==='count'){
     const old=s.stocks[key(c.itemId,c.to)]||{qty:0,version:0};
     if(old.version!==c.expected)fail('Остаток изменился во время пересчёта. Откройте пересчёт заново.');
     if(!(c.note||'').trim())fail('Укажите причину пересчёта.');
     e.changes=[{item:c.itemId,place:c.to,qty:c.qty-old.qty}];
    }
   }
   for(const d of e.changes){
    if(!s.places[d.place])fail('Место хранения не найдено.');
    const old=s.stocks[key(d.item,d.place)]||{item:d.item,place:d.place,qty:0,version:0};
    if(old.qty+d.qty<0)fail('Недостаточно остатка в выбранном месте. Операция не проведена.');
    if(old.qty+d.qty>MAX_QTY)fail('Превышен максимальный остаток.');
    s.stocks[key(d.item,d.place)]={...old,qty:old.qty+d.qty,version:old.version+1};
   }
   appendEvent(s,e);
  }else fail('Неизвестный вид операции.');
  s.seq++;rememberRecent(s.localSeen,c.id,intent);return s;
 }
 function validateBackup(raw){
  if(!raw||raw.format!=='yarus-data'||![1,2].includes(raw.version)||!raw.space||typeof raw.items!=='object'||typeof raw.places!=='object'||!Array.isArray(raw.events)||!raw.stocks)fail('Это не резервная копия ЯРУС.');
  const segments=raw.version===2?(raw.historySegments||[]):[];
  if(!Array.isArray(segments)||segments.some(segment=>!Array.isArray(segment)||!segment.length||segment.length>HISTORY_SEGMENT_SIZE)||raw.version===2&&raw.events.length>HISTORY_SEGMENT_SIZE)fail('В копии повреждены разделы истории.');
  const fullCount=segments.reduce((n,segment)=>n+segment.length,0)+raw.events.length;
  if(raw.eventCount!=null&&raw.eventCount!==fullCount)fail('В копии неполная история. Используйте полный экспорт владельца.');
  const s=createState(raw.space.name,false);s.space=copy(raw.space);if(s.space.id&&!validId(s.space.id))fail('Повреждён идентификатор склада.');s.space.id=s.space.id||newId();
  if(!text(s.space.name,80)||!s.space.name.trim()||!['RUB','USD','EUR'].includes(s.space.currency))fail('Повреждены настройки склада.');
  s.places={};
  if(Object.keys(raw.places).length<1||Object.keys(raw.places).length>200||Object.keys(raw.items).length>5000)fail('Превышены ограничения копии.');
  for(const [id,p] of Object.entries(raw.places)){
   if(id!==p.id||!validId(id)||!text(p.name,100)||!p.name.trim()||!text(p.note||'',200)||!Number.isInteger(p.version)||p.version<1)fail('Повреждено место хранения.');s.places[id]=copy(p);
  }
  for(const [id,x] of Object.entries(raw.items)){
   if(id!==x.id||!Number.isInteger(x.version)||x.version<1)fail('Повреждена карточка.');
   const checked=validateItem(s,{...x,archived:false});checked.version=x.version;checked.archived=!!x.archived;s.items[id]=checked;
  }
  const ids=new Set();s.stocks={};
  const source={historySegments:segments,events:raw.events};
  eachEvent(source,e=>{
   if(!validId(e.id)||ids.has(e.id)||!validId(e.item)||!s.items[e.item]||!['in','out','count','transfer','reverse'].includes(e.kind)||!Array.isArray(e.changes)||e.changes.length>2||!e.changes.length||!Number.isFinite(Date.parse(e.at))||!text(e.note||'',1000)||!text(e.actorName||'',80))fail('Повреждена история движений.');
   ids.add(e.id);
   for(const d of e.changes){
    if(d.item!==e.item||!validId(d.item)||!validId(d.place)||!s.items[d.item]||!s.places[d.place]||!Number.isSafeInteger(d.qty)||Math.abs(d.qty)>MAX_QTY)fail('Некорректное движение в копии.');
    const k=key(d.item,d.place),old=s.stocks[k]||{item:d.item,place:d.place,qty:0,version:0};
    if(old.qty+d.qty<0||old.qty+d.qty>MAX_QTY)fail('В истории обнаружен недопустимый остаток.');
    s.stocks[k]={...old,qty:old.qty+d.qty,version:old.version+1};
   }
  });
  for(const [k,x] of Object.entries(raw.stocks))if(!validId(x.item)||!validId(x.place)||!s.items[x.item]||!s.places[x.place]||k!==key(x.item,x.place)||!Number.isSafeInteger(x.qty)||x.qty!==(s.stocks[k]?.qty||0))fail('Остатки в копии не сходятся с историей.');
  for(const [k,x] of Object.entries(s.stocks))if(x.qty!==(raw.stocks[k]?.qty||0))fail('Копия содержит неполные остатки.');
  for(const x of Object.values(s.items))if(x.archived&&total(s,x.id))fail('Архивный товар имеет ненулевой остаток.');
  if(raw.version===2){s.historySegments=copy(segments);s.events=copy(raw.events);}else{s.historySegments=[];s.events=copy(raw.events);normalizeHistory(s);}
  s.eventCount=fullCount;s.seq=Number.isSafeInteger(raw.seq)?raw.seq:fullCount;return s;
 }
 function parseCSV(str){
  str=str.replace(/^\uFEFF/,'');const first=str.split(/\r?\n/)[0]||'';const delimiter=first.includes(';')?';':',';
  const rows=[];let row=[],cell='',quoted=false;
  for(let i=0;i<str.length;i++){
   const c=str[i];if(c==='"'){if(quoted&&str[i+1]==='"'){cell+='"';i++;}else if(quoted||cell==='')quoted=!quoted;else fail('Кавычка внутри неэкранированного поля CSV.');}
   else if(c===delimiter&&!quoted){row.push(cell);cell='';}
   else if((c==='\n'||c==='\r')&&!quoted){if(c==='\r'&&str[i+1]==='\n')i++;row.push(cell);if(row.some(x=>x.trim()))rows.push(row);row=[];cell='';}
   else cell+=c;
  }
  if(quoted)fail('В CSV не закрыты кавычки.');row.push(cell);if(row.some(x=>x.trim()))rows.push(row);return rows;
 }
 function csvCell(value){let s=String(value??'');if(/^[\s]*[=+\-@]/.test(s))s="'"+s;return '"'+s.replace(/"/g,'""')+'"';}
 const api={MAX_QTY,MAX_PRICE,HISTORY_SEGMENT_SIZE,RECENT_COMMAND_LIMIT,UNITS,PERMISSIONS,ROLE_PERMISSIONS,copy,validId,decimal,createState,newId,key,total,historyCount,allEvents,recentEvents,historySome,findEvent,countEvents,normalizeHistory,permissionsFor,can,execute,validateBackup,parseCSV,csvCell};
 if(typeof module!=='undefined'&&module.exports)module.exports=api;else root.YarusDomain=api;
})(typeof window!=='undefined'?window:globalThis);
