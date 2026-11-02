/* Optional workspace tools. Confirmed stock always goes through the shared command engine. */
'use strict';
const WORKSPACE_MODULES = {
  work: ['Рабочие инструменты', 'Инвентаризация и список пополнения.'],
  replenish: ['Пополнение запасов', 'Список недостающего до заданных минимумов.'],
  inventory: ['Инвентаризация', 'Пересчёт нескольких позиций одним документом.'],
  scanner: ['Камера и коды', 'Кнопки сканирования товаров и мест.'],
  map: ['Наглядные категории', 'Дополнительный вид каталога в виде полок.'],
  prices: ['Цены и оценка', 'Справочные цены и стоимость запасов.'],
  fields: ['Свои характеристики', 'Размер, материал, поставщик и другие поля карточки.']
};
const WORKSPACE_PROFILES = {
  home: {name:'Дом и личные вещи', description:'Вещи, расходники и места хранения.', modules:{work:true,replenish:true,inventory:true,scanner:true,map:false,prices:false,fields:true}},
  workshop: {name:'Мастерская и материалы', description:'Материалы, инструмент, минимумы и пересчёт.', modules:{work:true,replenish:true,inventory:true,scanner:true,map:false,prices:true,fields:true}},
  team: {name:'Склад и команда', description:'Запасы, документы движений и контроль остатков.', modules:{work:true,replenish:true,inventory:true,scanner:true,map:false,prices:true,fields:true}}
};
let countDraft=null, countDraftKey='', countDraftLoading=false, countDraftError='', countSaving=false, countSaveChain=Promise.resolve(), workTab='tools', workSearch='';
function moduleEnabled(key){return cfg.modules?.[key]!==false;}
function countUnit(row){return row.unit??state.items[row.item]?.unit??'';}
function countDifference(row){
  if(row.value==='')return {text:'Ещё не посчитано',kind:''};
  try{const diff=D.decimal(row.value)-row.qty;return {text:diff===0?'Сходится с учётом':(diff>0?'+':'')+q(diff)+' к учёту',kind:diff===0?'ok':'difference'};}
  catch{return {text:'Введите число от 0, до 3 знаков после запятой',kind:'error'};}
}
function updateCountFeedback(){
  if(!countDraft)return;
  for(const input of $$('.count-value')){
    const row=countDraft.rows.find(row=>row.item===input.dataset.id);if(!row)continue;
    const diff=countDifference(row),label=input.closest('.count-row');
    const item=state.items[row.item];label.firstElementChild.querySelector('small').textContent=(item?.sku||'Без артикула')+' · по учёту '+q(row.qty)+' '+countUnit(row);label.querySelector('.count-entry small').textContent=countUnit(row);
    let feedback=label.querySelector('.count-feedback');if(!feedback){feedback=document.createElement('small');feedback.className='count-feedback';feedback.id='count-feedback-'+row.item;label.firstElementChild.append(feedback);}
    feedback.textContent=diff.text;feedback.dataset.kind=diff.kind;input.setAttribute('aria-describedby',feedback.id);input.setAttribute('aria-invalid',String(diff.kind==='error'));
  }
  const filled=countDraft.rows.filter(row=>row.value!==''),changed=filled.filter(row=>countDifference(row).kind==='difference');
  const summary=$('.work-panel .tool-head p');if(summary)summary.textContent=`Посчитано ${filled.length} из ${countDraft.rows.length} · расхождений ${changed.length}`;
  const progress=$('.count-progress span');if(progress)progress.style.width=Math.round(filled.length/countDraft.rows.length*100)+'%';
}
function reviewCountDocument(draft,commands){
  const changes=commands.filter(command=>command.qty!==draft.rows.find(row=>row.item===command.itemId).qty);
  return new Promise(resolve=>{
    let done=false;const finish=value=>{if(done)return;done=true;observer.disconnect();resolve(value);};
    modal('Проверьте пересчёт',draft.name+' · '+placeName(draft.place),`<div class="stack"><div class="notice">Будет сохранено ${commands.length} посчитанных позиций, из них ${changes.length} с расхождениями. Непосчитанных: ${draft.rows.length-commands.length}. Их остатки сохранятся.</div><div class="review-count-list">${commands.map(command=>{const row=draft.rows.find(x=>x.item===command.itemId),item=state.items[row.item],diff=command.qty-row.qty;return `<div class="review-count-row"><div><strong>${esc(item.name)}</strong><small>${esc(item.sku||'Без артикула')}</small></div><span>${q(row.qty)} → <b>${q(command.qty)}</b> ${esc(item.unit)}<small>${diff?(diff>0?'+':'')+q(diff):'Без расхождения'}</small></span></div>`;}).join('')}</div><p class="small muted">Причина: ${esc(draft.note)}</p><div class="notice">Все строки проходят проверку вместе. Если другой участник изменил посчитанный остаток, документ целиком будет отклонён.</div></div>`,`<button class="btn" data-action="close-modal">Вернуться к цифрам</button><button class="btn primary" id="count-review-submit">${ic('check')}Провести документ</button>`,true);
    const observer=new MutationObserver(()=>{if(!$('#count-review-submit'))finish(false);});observer.observe($('#modal-root'),{childList:true,subtree:true});
    $('#count-review-submit').addEventListener('click',()=>{finish(true);closeModal(true);});
  });
}
function workspaceStorageKey(){return 'stocktake:'+activeKey()+':'+state.space.id;}
function batchAvailable(){return !isShared()||state.capabilities?.batch===true;}
function applyWorkspaceProfile(key){
  const profile=WORKSPACE_PROFILES[key];if(!profile)return;
  cfg.profile=key;cfg.modules={...profile.modules};saveCfg();
  $('#app').innerHTML='';render();toast('Набор «'+profile.name+'» применён. Данные и права доступа сохранены.');
}
function workspaceSettingsHTML(){
  return `<section class="card settings-card workspace-settings"><h2>Под ваши задачи</h2><p>Настройте этот экран и инструменты на устройстве. Скрытие функции сохраняет её данные и не меняет права участников.</p><div class="profile-options">${Object.entries(WORKSPACE_PROFILES).map(([key,p])=>`<button class="profile-option ${cfg.profile===key?'selected':''}" data-action="workspace-profile" data-id="${key}"><strong>${p.name}</strong><small>${p.description}</small></button>`).join('')}</div><details class="module-options" open><summary>Какие инструменты показывать</summary>${Object.entries(WORKSPACE_MODULES).map(([key,[name,description]])=>`<label class="setting-row"><span><strong>${name}</strong><small>${description}</small></span><input class="switch workspace-module" type="checkbox" data-module="${key}" aria-label="${name}" ${moduleEnabled(key)?'checked':''}></label>`).join('')}</details><div class="setting-row"><label for="workspace-size">Размер текста</label><select id="workspace-size"><option value="normal" ${cfg.textSize!=='large'?'selected':''}>Обычный</option><option value="large" ${cfg.textSize==='large'?'selected':''}>Крупный</option></select></div><div class="setting-row"><label for="workspace-page-size">Строк на странице</label><select id="workspace-page-size">${[30,60,120].map(n=>`<option value="${n}" ${Number(cfg.pageSize||60)===n?'selected':''}>${n}</option>`).join('')}</select></div><div class="setting-row"><label for="workspace-start">При запуске открывать</label><select id="workspace-start">${[['home','Обзор'],['stock','Каталог'],['work','Работа']].map(([key,name])=>`<option value="${key}" ${cfg.startPage===key?'selected':''}>${name}</option>`).join('')}</select></div></section>`;
}
function modernHomeView(){
  const items=activeItems(),low=lowItems(),recent=D.recentEvents(state,4).reverse(),available=items.filter(x=>itemTotal(x,'')>0).length;
  const metric=(key,title,value,note,icon)=>`<article class="card metric ${key==='value'?'accent':''}" data-widget="${key}"><div class="metric-top"><span>${title}</span>${ic(icon)}</div><div class="metric-value ${key==='value'?'money':''}">${value}</div><div class="metric-bottom">${note}</div></article>`;
  return `${banners()}<div class="page-head home-head"><div><div class="eyebrow">${esc(state.space.name)} · ${isShared()?'общая база':'локальная база'}</div><h1>Вещи, материалы, запасы.</h1><p>Всё нужное для работы — на вашем устройстве.</p></div><div class="head-actions home-actions"><button class="btn primary" data-action="operation" data-kind="in" ${disabledWrite()}>${ic('in')}Приход</button><button class="btn" data-action="operation" data-kind="out" ${disabledWrite()}>${ic('out')}Расход</button>${moduleEnabled('scanner')?`<button class="btn" data-action="scan-code">${ic('barcode')}Сканировать</button>`:''}</div></div><section class="workspace-status"><span class="status-symbol">${ic(isShared()?'wifi':'shield')}</span><div class="grow"><strong>${esc(currentStatus())}</strong><p>${isShared()?'Одно главное устройство подтверждает изменения. Остальные работают с ним по сети.':'Каталог и история доступны без интернета. Сохраните копию отдельно от устройства.'}</p></div><button class="linkbtn" data-action="sync-status">Подробнее ${ic('arrow')}</button></section><section class="metrics home-metrics" aria-label="Главное о складе">${metric('items','В наличии',available,'из '+countLabel(items.length,['позиции','позиций','позиций']),'box')}${metric('low','Нужно пополнить',low.length,'нулевой остаток или достигнут минимум','bell')}${metric('today','Движений сегодня',D.countEvents(state,x=>today(x.at)),'с именем участника и временем','history')}${moduleEnabled('prices')&&can('view_prices')?metric('value','Оценка запасов',money(valuation(),true),'по справочным ценам карточек','money'):''}</section><nav class="workspace-shortcuts" aria-label="Быстрые разделы"><button class="card workspace-shortcut" data-nav="stock">${ic('box')}<span><strong>Каталог и остатки</strong><small>Поиск, категории и места</small></span>${ic('arrow')}</button>${moduleEnabled('work')?`<button class="card workspace-shortcut" data-nav="work">${ic('count')}<span><strong>Работа с запасами</strong><small>Пополнение и инвентаризация</small></span>${ic('arrow')}</button>`:''}<button class="card workspace-shortcut" data-nav="places">${ic('place')}<span><strong>Места хранения</strong><small>${countLabel(Object.keys(state.places).length,['место','места','мест'])}</small></span>${ic('arrow')}</button>${moduleEnabled('map')?`<button class="card workspace-shortcut" data-action="open-shelf">${ic('layers')}<span><strong>Наглядные категории</strong><small>Открыть полки каталога</small></span>${ic('arrow')}</button>`:''}</nav><section class="two-col home-details"><article class="card home-attention-card"><div class="card-head"><h2>Внимание к запасам <span class="tag warn">${low.length}</span></h2><button class="linkbtn" data-action="show-low">Открыть</button></div>${low.length?`<div class="attention-list">${low.slice(0,5).map(x=>`<div class="attention-row"><button class="btn quiet iconbtn" data-action="detail" data-id="${x.id}" aria-label="Открыть ${esc(x.name)}">${itemIcon(x)}</button><div class="grow"><button class="linkbtn item-title" data-action="detail" data-id="${x.id}">${esc(x.name)}</button><div class="item-sub">${esc(x.category||'Без категории')}</div></div><span class="stock-qty">${q(itemTotal(x,''))}<small>${esc(x.unit)}</small></span></div>`).join('')}</div>`:empty(items.length?'Запасов достаточно':'Начните с первой карточки',items.length?'Товары с недостатком появятся здесь.':'Добавьте вещь, материал или товар, затем укажите количество.',items.length?'':`<button class="btn primary" data-action="new-item" ${disabledWrite('catalog')}>Добавить позицию</button>`,'check')}</article><article class="card workspace-recent"><div class="card-head"><h2>Последние изменения</h2><button class="linkbtn" data-nav="history">Вся история</button></div>${recent.length?`<div class="activity">${recent.map(e=>eventRow(e)).join('')}</div>`:empty('История сохраняется сама','Приходы, расходы и пересчёты останутся в журнале.','','history')}</article></section>`;
}
function replenishmentItems(){return activeItems().map(item=>({item,qty:itemTotal(item,''),need:Math.max(0,item.min-itemTotal(item,''))})).filter(x=>x.qty===0||x.item.min>0&&x.qty<=x.item.min);}
function replenishmentView(){
  const all=replenishmentItems(),term=workSearch.trim().toLowerCase(),rows=all.filter(x=>[x.item.name,x.item.sku,x.item.category].join(' ').toLowerCase().includes(term));
  return `<div class="tool-head"><div><h2>Что нужно пополнить</h2><p>Общий остаток по всем местам. Количество к пополнению — до минимума, заданного в карточке.</p></div><button class="btn" data-action="replenish-export">${ic('download')}Сохранить список CSV</button></div><label class="field">Поиск в списке<input class="input" id="work-search" value="${esc(workSearch)}" placeholder="Название, категория, артикул"></label>${rows.length?`<div class="replenish-list">${rows.slice(0,500).map(({item,qty,need})=>`<article class="replenish-row"><div class="grow"><button class="linkbtn item-title" data-action="detail" data-id="${item.id}">${esc(item.name)}</button><small>${esc(item.category||'Без категории')} · ${esc(item.sku||'Без артикула')}</small></div><div><small>Сейчас</small><strong>${q(qty)} ${esc(item.unit)}</strong></div><div><small>До минимума</small><strong>${item.min>0?(need>0?q(need)+' '+esc(item.unit):'Минимум достигнут'):'Минимум не задан'}</strong></div></article>`).join('')}</div><p class="small muted">Показано ${Math.min(rows.length,500)} из ${rows.length}. CSV сохраняет весь список с учётом поиска. Измените минимум в карточке, чтобы настроить пополнение.</p>`:empty('Список пуст','Все заданные минимумы соблюдены или по поиску нет совпадений.','','check')}`;
}
async function loadCountDraft(){
  const key=workspaceStorageKey();if(countDraftKey===key||countDraftLoading)return;
  countDraftLoading=true;countDraftKey=key;countDraftError='';
  try{const value=await getKV(key);if(state&&workspaceStorageKey()===key)countDraft=value||null;}
  catch(e){countDraftKey='';countDraftError='Не удалось прочитать черновик: '+e.message;}
  finally{countDraftLoading=false;if(page==='work')render();}
}
function saveCountDraft(){
  const key=countDraftKey,value=countDraft?D.copy(countDraft):null;
  countSaveChain=countSaveChain.catch(()=>{}).then(()=>value?putKV(key,value):delKV(key));
  return countSaveChain;
}
async function workspaceCommandAccepted(command,key){
  if(!key||command.type!=='batch')return;
  const stored=await getKV(key);
  if(stored?.id!==command.id||stored.command?.id!==command.id)return;
  await delKV(key);
  if(countDraftKey===key&&countDraft?.id===command.id){countDraft=null;workTab='tools';}
}
function inventoryView(){
  if(countDraftLoading)return '<p role="status">Открываем сохранённый пересчёт…</p>';
  if(countDraftError)return `<div class="notice warn">${esc(countDraftError)}</div>`;
  if(!countDraft)return `<div class="tool-head"><div><h2>Инвентаризация без спешки</h2><p>Выберите место, посчитайте позиции и проверьте расхождения. Факт сохранится одним документом.</p></div></div><form id="count-start" class="count-start"><label class="field">Где считаете<select name="place">${placeOptions(placeFilter)}</select></label><label class="field">Категория<select name="category"><option value="">Все категории</option>${[...new Set(activeItems().map(x=>x.category).filter(Boolean))].sort().map(x=>`<option>${esc(x)}</option>`).join('')}</select></label><label class="field">Какие позиции<select name="scope"><option value="present">С ненулевым остатком в этом месте</option><option value="all">Весь каталог выбранной категории</option></select></label><label class="field">Название документа<input class="input" name="name" maxlength="100" value="Инвентаризация ${new Date().toLocaleDateString('ru-RU')}"></label><div class="notice">До ${D.LIMITS?.batchRows||500} позиций в документе. Черновик хранится на этом устройстве и сохраняется при каждом вводе. В общую базу попадёт только подтверждённый результат.</div><button class="btn primary" ${disabledWrite()}>${ic('count')}Начать пересчёт</button></form>`;
  const draft=countDraft,filled=draft.rows.filter(x=>x.value!==''),changed=filled.filter(x=>{try{return D.decimal(x.value)!==x.qty;}catch{return true;}}),pending=!!draft.command;
  return `<div class="tool-head"><div><span class="eyebrow">${esc(placeName(draft.place))}</span><h2>${esc(draft.name)}</h2><p>Посчитано ${filled.length} из ${draft.rows.length} · расхождений ${changed.length}</p></div><button class="btn" data-action="count-export">${ic('download')}Ведомость CSV</button></div>${pending?'<div class="notice warn">Документ подготовлен к отправке. Поля заблокированы, чтобы повтор не создал второе движение. Проверьте синхронизацию или повторите отправку того же документа.</div>':'<div class="notice">Пустое поле означает «ещё не посчитано». Введите 0 только если позиции действительно нет. Уже введённые значения сохраняются на этом устройстве.</div>'}<div class="count-progress"><span style="width:${draft.rows.length?Math.round(filled.length/draft.rows.length*100):0}%"></span></div><div class="count-rows">${draft.rows.map(row=>{const item=state.items[row.item];return `<label class="count-row"><span><strong>${esc(item?.name||'Позиция недоступна')}</strong><small>${esc(item?.sku||'Без артикула')} · по учёту ${q(row.qty)} ${esc(item?.unit||'')}</small></span><span class="count-entry"><input class="input count-value" data-id="${row.item}" aria-label="Факт: ${esc(item?.name||row.item)}" inputmode="decimal" value="${esc(row.value)}" placeholder="Не считали" ${pending?'disabled':''}><small>${esc(item?.unit||'')}</small></span></label>`;}).join('')}</div><form id="count-finish" class="stack"><label class="field">Причина и комментарий<textarea name="note" id="count-note" maxlength="1000" ${pending?'disabled':''}>${esc(draft.note||'Плановая инвентаризация')}</textarea></label><div class="row wrap"><button class="btn primary" ${disabledWrite()} ${countSaving?'disabled':''}>${ic('check')}${pending?'Повторить отправку документа':'Проверить и провести'}</button><button type="button" class="btn" data-action="count-cancel">${pending?'Разобрать отправку':'Удалить черновик'}</button><span id="count-save-status" class="small muted" role="status">Черновик на этом устройстве</span></div></form>`;
}
function workView(){
  return `${banners()}<div class="page-head"><div><div class="eyebrow">Рабочие инструменты</div><h1>Меньше рутины. Больше порядка.</h1><p>Планируйте пополнение и сверяйте фактические остатки.</p></div><button class="btn" data-nav="settings">${ic('settings')}Настроить инструменты</button></div><div class="tabs work-tabs">${[['tools','Инструменты'],...(moduleEnabled('replenish')?[['replenish','Пополнение']]:[]),...(moduleEnabled('inventory')?[['inventory','Инвентаризация']]:[])].map(([key,name])=>`<button class="tab ${workTab===key?'active':''}" data-action="work-tab" data-id="${key}">${name}</button>`).join('')}</div><section class="card work-panel">${workTab==='replenish'&&moduleEnabled('replenish')?replenishmentView():workTab==='inventory'&&moduleEnabled('inventory')?inventoryView():`<div class="work-tools">${moduleEnabled('replenish')?`<button class="tool-card" data-action="work-tab" data-id="replenish">${ic('bell')}<strong>Пополнение</strong><p>${replenishmentItems().length} позиций требуют внимания. Сохраните список для покупки или заказа.</p><span>Открыть список →</span></button>`:''}${moduleEnabled('inventory')?`<button class="tool-card" data-action="work-tab" data-id="inventory">${ic('count')}<strong>Инвентаризация</strong><p>${countDraft?'Есть сохранённый черновик пересчёта.':'Пересчитайте место хранения и проведите расхождения одним документом.'}</p><span>${countDraft?'Продолжить':'Начать пересчёт'} →</span></button>`:''}<button class="tool-card" data-nav="history">${ic('history')}<strong>Журнал движений</strong><p>Найдите нужное изменение по дате, участнику или документу.</p><span>Открыть историю →</span></button></div>`}</section>`;
}
function enhanceWorkspace(){
  document.documentElement.dataset.textSize=cfg.textSize||'normal';
  if(!moduleEnabled('map')){for(const e of $$('[data-action="open-shelf"],.nav [data-nav="shelf"]'))e.hidden=true;}
  if(!moduleEnabled('scanner')){for(const e of $$('[data-action="scan-code"]'))e.hidden=true;}
  if(!moduleEnabled('prices')){for(const e of $$('.metric[data-widget="value"]'))e.hidden=true;$('.inventory-table')?.classList.add('prices-hidden');}
  if(page==='settings'){
    $('.settings-grid')?.insertAdjacentHTML('afterbegin',workspaceSettingsHTML());
    $('.settings-grid')?.insertAdjacentHTML('beforeend',`<section class="card settings-card"><h2>Объём данных</h2><div class="capacity-line"><strong>${Object.keys(state.items).length.toLocaleString('ru-RU')}</strong><span>из ${(D.LIMITS?.items||50000).toLocaleString('ru-RU')} карточек, включая архив</span></div><div class="capacity-line"><strong>${Object.keys(state.places).length.toLocaleString('ru-RU')}</strong><span>из ${(D.LIMITS?.places||2000).toLocaleString('ru-RU')} мест хранения</span></div><p>История не обрезается по числу записей. Фактический объём зависит от памяти и свободного места устройства. Перед крупным импортом сохраните полную копию.</p><button class="btn" data-action="backup" ${disabledWrite('full_export')}>${ic('download')}Сохранить полную копию</button></section>`);
  }
  if(page==='work'){
    if(countDraftKey!==workspaceStorageKey()&&!countDraftLoading){countDraft=null;void loadCountDraft();}
    $('#count-start')?.addEventListener('submit',startInventory);
    $('#count-finish')?.addEventListener('submit',finishInventory);
    updateCountFeedback();
  }
}
async function startInventory(event){
  event.preventDefault();if(!can('stock'))return;
  if(!batchAvailable()){toast('Для документов инвентаризации обновите главное устройство до ЯРУС 2.0. Обычные движения доступны.','warn');return;}
  const data=new FormData(event.currentTarget),place=String(data.get('place')),category=String(data.get('category'));
  const items=activeItems().filter(x=>(!category||x.category===category)&&(data.get('scope')==='all'||itemTotal(x,place)>0));
  if(!items.length){toast('В выбранном месте и категории нет позиций. Выберите весь каталог или другую категорию.','warn');return;}
  if(items.length>(D.LIMITS?.batchRows||500)){toast('В этом наборе '+items.length+' позиций. Выберите категорию: один документ вмещает до '+(D.LIMITS?.batchRows||500)+'.','warn');return;}
  countDraft={id:id(),name:String(data.get('name')).trim()||'Инвентаризация',place,createdAt:new Date().toISOString(),note:'Плановая инвентаризация',rows:items.map(x=>({item:x.id,unit:x.unit,qty:itemTotal(x,place),expected:state.stocks[D.key(x.id,place)]?.version||0,value:'',id:id()}))};
  countDraftKey=workspaceStorageKey();try{await saveCountDraft();render();}catch(e){countDraft=null;toast('Не удалось сохранить черновик: '+e.message,'error');}
}
async function finishInventory(event){
  event.preventDefault();if(countSaving||!countDraft||!can('stock'))return;
  const draft=countDraft,draftKey=countDraftKey;countSaving=true;event.currentTarget.querySelector('button[type=submit],button:not([type])')?.setAttribute('disabled','');
  try{
    await countSaveChain.catch(()=>{});
    await saveCountDraft();
    if(!draft.command){
      const note=(draft.note||'').trim();if(!note)throw new Error('Укажите причину пересчёта.');
      const counted=draft.rows.filter(x=>x.value!=='');if(!counted.length)throw new Error('Введите фактическое количество хотя бы для одной позиции.');
      const commands=counted.map(row=>({id:row.id,type:'count',itemId:row.item,to:draft.place,qty:D.decimal(row.value),expected:row.expected,expectedUnit:countUnit(row),note,ref:draft.name}));
      // Checking every counted row also checks unchanged counts for concurrent writes.
      for(const row of counted)if((state.stocks[D.key(row.item,draft.place)]?.version||0)!==row.expected)throw new Error('Остатки изменились после начала пересчёта. Сохраните ведомость и начните новый пересчёт, чтобы не затереть чужое изменение.');
      for(const row of counted)if(state.items[row.item]?.unit!==countUnit(row))throw new Error('Единица измерения изменилась после начала пересчёта. Сохраните ведомость и начните новый пересчёт.');
      if(!await reviewCountDocument(draft,commands))return;
      if(countDraftKey!==draftKey||countDraft?.id!==draft.id)throw new Error('Открыт другой черновик. Вернитесь в нужный склад.');
      const command={id:draft.id,type:'batch',commands,note,ref:draft.name};D.execute(state,command);
      draft.command=command;await saveCountDraft();
    }
    // Keep the exact payload durable before every attempt, including retries
    // after a failed storage write. Never send a document we cannot recover.
    if(countDraftKey!==draftKey||countDraft?.id!==draft.id)throw new Error('Открыт другой черновик. Вернитесь в нужный склад.');
    await saveCountDraft();
    const result=await send(draft.command);
    if(result.saved){await workspaceCommandAccepted(draft.command,draftKey);render();toast('Инвентаризация проведена. Все строки сохранены в истории.');}
    else{render();toast('Документ сохранён в очереди. Остатки изменятся после подтверждения главным устройством.','warn');}
  }catch(e){toast(e.message,'error');render();}finally{countSaving=false;$('#count-finish button')?.removeAttribute('disabled');}
}
function csvDownload(name,rows){downloadText(name,'\uFEFF'+rows.map(row=>row.map(D.csvCell).join(';')).join('\r\n'),'text/csv');}
async function workspaceAction(name,element){
  switch(name){
    case 'workspace-profile':applyWorkspaceProfile(element.dataset.id);return true;
    case 'work-tab':workTab=element.dataset.id;page='work';render();window.scrollTo(0,0);return true;
    case 'replenish-export':{
      const term=workSearch.trim().toLowerCase(),rows=replenishmentItems().filter(x=>[x.item.name,x.item.sku,x.item.category].join(' ').toLowerCase().includes(term));
      csvDownload('YARUS-пополнение.csv',[['Название','Артикул','Категория','Остаток','Минимум','До минимума','Единица'],...rows.map(x=>[x.item.name,x.item.sku,x.item.category,decimalString(x.qty),decimalString(x.item.min),x.item.min>0?decimalString(x.need):'Минимум не задан',x.item.unit])]);return true;
    }
    case 'count-export':if(countDraft)csvDownload('YARUS-инвентаризация.csv',[['Документ','Место','Название','Артикул','По учёту','Факт','Разница','Единица'],...countDraft.rows.map(row=>{const item=state.items[row.item];let diff='';try{if(row.value!=='')diff=decimalString(D.decimal(row.value)-row.qty);}catch{}return [countDraft.name,placeName(countDraft.place),item?.name,item?.sku,decimalString(row.qty),row.value,diff,countUnit(row)];})]);return true;
    case 'count-cancel':{
      if(!countDraft)return true;
      if(countDraft.command){
        const entry=queue.find(x=>x.command.id===countDraft.command.id);
        if(entry&&!entry.error){queueModal();return true;}
        if(!entry&&cfg.mode==='remote'){await syncNow();const latest=D.recentEvents(state,1000);if(latest.some(x=>countDraft.rows.some(row=>row.id===x.id))){countDraft=null;await saveCountDraft();render();toast('Документ уже подтверждён. Черновик закрыт.');return true;}}
      }
      if(confirm('Удалить черновик пересчёта на этом устройстве? Сохраните ведомость CSV, если введённые цифры нужны. Подтверждённые движения останутся в истории.')){
        if(countDraft.command)await mutateQueue(rows=>rows.filter(x=>x.command.id!==countDraft.command.id));
        countDraft=null;await saveCountDraft();render();
      }return true;
    }
    default:return false;
  }
}
document.addEventListener('change',event=>{
  const e=event.target;
  if(e.matches('.workspace-module')){cfg.modules={...cfg.modules,[e.dataset.module]:e.checked};cfg.profile='custom';saveCfg();$('#app').innerHTML='';render();}
  else if(e.id==='workspace-size'){cfg.textSize=e.value;saveCfg();render();}
  else if(e.id==='workspace-page-size'){cfg.pageSize=Number(e.value);saveCfg();render();}
  else if(e.id==='workspace-start'){cfg.startPage=e.value;saveCfg();}
});
document.addEventListener('input',event=>{
  const e=event.target;
  if(e.id==='work-search'){const pos=e.selectionStart;workSearch=e.value;render();$('#work-search')?.focus();$('#work-search')?.setSelectionRange(pos,pos);}
  if(!countDraft||countDraft.command)return;
  if(e.matches('.count-value')){const row=countDraft.rows.find(row=>row.item===e.dataset.id);if(row)row.value=e.value.trim();}
  else if(e.id==='count-note')countDraft.note=e.value;
  else return;
  updateCountFeedback();
  const status=$('#count-save-status');if(status)status.textContent='Сохраняем черновик…';
  void saveCountDraft().then(()=>{if(status?.isConnected)status.textContent='Черновик сохранён на устройстве';}).catch(error=>{if(status?.isConnected)status.textContent='Ошибка сохранения';toast('Черновик не сохранён: '+error.message,'error');});
});
