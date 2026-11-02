'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),D=require('../web/domain.js');
const card=(index,version=0)=>({id:'capacity-item-'+String(index).padStart(6,'0'),name:'Позиция '+index,sku:'SKU-'+index,barcode:'',category:'Материалы',unit:'шт',min:0,price:0,note:'',fields:{},archived:false,version});
function seeded(){return D.execute(D.createState('Документы'),{id:'catalog-command-001',type:'item',item:card(1)});}

test('50k-card backup restores with indexed validation and accepts exactly the declared capacity',()=>{
 const raw=D.createState('Большой каталог');
 for(let i=0;i<D.LIMITS.items;i++){const item=card(i,1);raw.items[item.id]=item;}
 for(let i=1;i<D.LIMITS.places;i++)raw.places['capacity-place-'+i]={id:'capacity-place-'+i,name:'Ячейка '+i,note:'',version:1};
 const before=performance.now(),restored=D.validateBackup(raw);
 assert.equal(Object.keys(restored.items).length,50000);assert.equal(Object.keys(restored.places).length,2000);
 console.log('50k-card / 2k-place restore: '+Math.round(performance.now()-before)+' ms');
 assert.throws(()=>D.execute(restored,{id:'over-capacity-001',type:'item',item:card(50001)}),/50 000/);
 assert.throws(()=>D.execute(restored,{id:'over-place-capacity',type:'place',place:{id:'one-too-many-place',name:'Лишнее место',note:'',version:0}}),/2000/);
 const duplicate={...raw,items:{...raw.items}};duplicate.items[card(49999).id]={...raw.items[card(49999).id],sku:'sku-0'};
 assert.throws(()=>D.validateBackup(duplicate),/уже есть/);
});

test('stock totals are indexed and cache is invalidated after movements',()=>{
 const state=seeded(),first=D.execute(state,{id:'total-first-receipt',type:'in',itemId:card(1).id,to:'main-place',qty:1000});
 assert.equal(D.total(first,card(1).id),1000);
 const second=D.execute(first,{id:'total-next-receipt',type:'in',itemId:card(1).id,to:'main-place',qty:2000});
 assert.equal(D.total(second,card(1).id),3000);assert.equal(D.total(first,card(1).id),1000);
 assert.equal(D.total(second,card(1).id,'main-place'),3000);assert.equal(D.total(second,card(1).id,'empty-place'),0);
});

test('catalog editors without price access preserve stored prices and cannot set new prices',()=>{
 const owner=seeded(),state=D.execute(owner,{id:'price-owner-update',type:'item',item:{...owner.items[card(1).id],price:12345}}),actor={id:'catalog-manager1',role:'manager',name:'Менеджер',permissions:{view_prices:false}};
 const edited=D.execute(state,{id:'price-hidden-edit1',type:'item',item:{...state.items[card(1).id],name:'Новое название',price:0}},actor);
 assert.equal(edited.items[card(1).id].price,12345);assert.equal(edited.items[card(1).id].name,'Новое название');
 const created=D.execute(edited,{id:'price-hidden-new01',type:'item',item:{...card(2),price:99999}},actor);assert.equal(created.items[card(2).id].price,0);
 const imported=D.execute(created,{id:'price-hidden-import',type:'catalog-import',items:[{...card(3),price:99999}]},actor);assert.equal(imported.items[card(3).id].price,0);
 assert.equal(state.items[card(1).id].price,12345);
});

test('multi-line document is atomic, permission checked and retry-safe',()=>{
 const state=seeded(),before=D.copy(state),command={id:'document-command-001',type:'batch',ref:'Приход 17',note:'Поставка',commands:[
  {id:'document-row-001',type:'in',itemId:card(1).id,to:'main-place',qty:5000},
  {id:'document-row-002',type:'out',itemId:card(1).id,from:'main-place',qty:2000}
 ]};
 const applied=D.execute(state,command);assert.equal(D.total(applied,card(1).id),3000);assert.equal(applied.seq,state.seq+1);
 assert.equal(applied.eventCount,2);assert.equal(applied.events[0].seq,applied.events[1].seq);assert.equal(applied.events[0].ref,'Приход 17');
 assert.deepEqual(D.execute(applied,command),applied);assert.deepEqual(state,before);
 const broken={...command,id:'broken-document-001',commands:[command.commands[0],{...command.commands[1],qty:6000}]};
 assert.throws(()=>D.execute(state,broken),/Недостаточно/);assert.deepEqual(state,before);
 assert.throws(()=>D.execute(state,command,{id:'viewer-001',role:'viewer',name:'Читатель'}),/нет права/);
 assert.throws(()=>D.execute(state,{...command,commands:[command.commands[0],command.commands[0]]}),/повторяется/);
 assert.throws(()=>D.execute(applied,{...command,id:'duplicate-document-002'}),/повторяется/);
 assert.equal(D.total(D.validateBackup(applied),card(1).id),3000);
});

test('stocktaking document rolls back all rows on a stale count',()=>{
 const state=D.execute(seeded(),{id:'count-start-receipt',type:'in',itemId:card(1).id,to:'main-place',qty:5000}),before=D.copy(state);
 const command={id:'stocktaking-command',type:'batch',note:'Пересчёт',commands:[
  {id:'stocktaking-row-001',type:'count',itemId:card(1).id,to:'main-place',qty:4000,expected:1},
  {id:'stocktaking-row-002',type:'count',itemId:card(1).id,to:'main-place',qty:3000,expected:1}
 ]};
 assert.throws(()=>D.execute(state,command),/изменился/);assert.deepEqual(state,before);
});

test('stocktaking rejects a changed unit even before the first movement and accepts legacy commands',()=>{
 const original=seeded(),state=D.execute(original,{id:'changed-unit-card',type:'item',item:{...original.items[card(1).id],unit:'кг'}}),before=D.copy(state);
 const row={id:'unit-count-row01',type:'count',itemId:card(1).id,to:'main-place',qty:3000,expected:0,expectedUnit:'шт',note:'Пересчёт'};
 assert.throws(()=>D.execute(state,row),/Единица учёта изменилась/);
 assert.throws(()=>D.execute(state,{id:'unit-count-batch',type:'batch',commands:[{...row,id:'unit-count-row02',expectedUnit:'кг'},row]}),/Единица учёта изменилась/);
 assert.deepEqual(state,before);
 assert.equal(D.total(D.execute(state,{...row,expectedUnit:'кг'}),card(1).id),3000);
 const legacy={...row};delete legacy.expectedUnit;
 assert.equal(D.total(D.execute(state,legacy),card(1).id),3000);
});

test('10k CSV catalog import validates and saves atomically without repeated state copies',()=>{
 const state=seeded(),items=Array.from({length:10000},(_,index)=>card(index+2)),before=D.copy(state);
 const started=performance.now(),prepared=D.prepareCatalogImport(state,items);
 assert.equal(prepared.length,10000);assert.equal(prepared[0].version,0);assert.deepEqual(state,before);
 const command={id:'bulk-catalog-import',type:'catalog-import',items:prepared},result=D.execute(state,command);
 assert.equal(Object.keys(result.items).length,10001);assert.equal(result.seq,state.seq+1);assert.equal(result.items[items[0].id].version,1);
 console.log('10k catalog validation + atomic import: '+Math.round(performance.now()-started)+' ms');
 assert.deepEqual(D.execute(result,command),result);assert.deepEqual(state,before);
 const duplicate=[card(9),{...card(10),sku:'sku-9'}];assert.throws(()=>D.execute(state,{...command,items:duplicate}),/уже есть/);assert.deepEqual(state,before);
 assert.throws(()=>D.execute(state,command,{id:'operator-001',role:'operator',name:'Кладовщик'}),/нет права/);
 assert.throws(()=>D.execute(state,{...command,items:Array(10001).fill(card(2))}),/10 000/);
});
