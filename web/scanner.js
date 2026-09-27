/* Shared scanner workflow. Android uses the native camera; Web and Windows keep an offline fallback. */
let yarusScanSession=null;

function closeScanner(){
 const s=yarusScanSession;if(!s)return;s.closed=true;clearTimeout(s.timer);s.stream?.getTracks().forEach(track=>track.stop());s.root.remove();document.removeEventListener('keydown',s.keys,true);yarusScanSession=null;document.body.style.overflow=s.previousOverflow;s.focus?.focus?.({preventScroll:true});
}

function readScanImage(file){
 return new Promise((resolve,reject)=>{const fail=()=>reject(new Error('Не удалось открыть изображение. Выберите JPG, PNG или WEBP.')),reader=new FileReader();reader.onerror=fail;reader.onabort=fail;reader.onload=()=>{const image=new Image();image.onload=()=>resolve(image);image.onerror=fail;image.src=String(reader.result||'');};try{reader.readAsDataURL(file);}catch{fail();}});
}

function nativeScannerAvailable(){
 try{return !!window.AndroidFiles?.nativeScannerAvailable?.();}catch{return false;}
}

function scanHint(target){
 if(target==='invite')return 'Наведите камеру на QR-приглашение.';
 if(target==='barcode')return 'Считайте код с упаковки.';
 if(target==='item')return 'Наведите камеру на код товара.';
 if(target==='place'||target==='destination')return 'Наведите камеру на QR места.';
 return 'Наведите камеру на один код.';
}

function openScanner(target='lookup',autoStart=false){
 closeScanner();
 const root=document.createElement('div'),oldFocus=document.activeElement,native=nativeScannerAvailable();
 root.id='scan-root';root.className='scan-backdrop';
 root.innerHTML=`<section class="scan-panel${native?' native':''}" role="dialog" aria-modal="true" aria-labelledby="scan-title"><header class="scan-header"><div><h2 id="scan-title">Сканер</h2><p>${esc(scanHint(target))}</p></div><button class="btn iconbtn quiet" id="scan-close" aria-label="Закрыть сканер">${ic('close')}</button></header><div class="scan-body"><div class="scan-viewport"${native?' hidden':''}><video id="scan-video" playsinline muted></video><div class="scan-guide" aria-hidden="true"><span></span></div><div id="scan-cover">${ic('barcode')}<p>Поместите код в рамку</p></div></div><p id="scan-status" role="status" aria-live="polite">${native?'Камера телефона откроется на весь экран.':'Камера включится сразу.'}</p><div class="scan-actions"><button class="btn primary" id="scan-start">${ic('device')}${native?'Открыть камеру':'Включить камеру'}</button><button class="btn" id="scan-torch" hidden>Фонарик</button><label class="btn" for="scan-image">${ic('file')}Фото</label><input type="file" id="scan-image" accept="image/*" class="visually-hidden"></div><label class="field scan-camera-choice" id="scan-camera-choice" hidden>Другая камера<select id="scan-cameras" aria-label="Выбор камеры"></select></label><label class="field scan-zoom" id="scan-zoom-wrap" hidden>Приближение<div class="row"><input id="scan-zoom" type="range"><output id="scan-zoom-value"></output></div></label><div id="scan-result" aria-live="polite"></div><details class="scan-manual-details"><summary>Ввести код вручную или внешним сканером</summary><form id="scan-manual-form" class="scan-manual"><label class="field">Код<input id="scan-manual" class="input" autocomplete="off" autocapitalize="none" spellcheck="false" maxlength="512" placeholder="Введите код" aria-label="Код вручную"></label><button class="btn" type="submit">Готово</button></form></details><details class="scan-help small"><summary>Какие коды подходят</summary><p>QR ЯРУС, EAN-13, EAN-8 и Code 128. Для USB- или Bluetooth-сканера откройте ручной ввод.</p></details></div></section>`;
 document.body.append(root);
 const s={root,target,native,closed:false,stream:null,timer:null,focus:oldFocus,previousOverflow:document.body.style.overflow,generation:0,nativePending:false,torch:false};
 yarusScanSession=s;document.body.style.overflow='hidden';
 const el=id=>root.querySelector('#'+id);
 const status=(text,error=false)=>{const node=el('scan-status');if(!node)return;node.textContent=text;node.className=error?'error-text':'';};
 const clearResult=()=>{const node=el('scan-result');if(node)node.innerHTML='';};
 s.keys=event=>{if(event.key==='Escape'){event.preventDefault();event.stopImmediatePropagation();closeScanner();return;}if(event.key!=='Tab')return;const list=[...root.querySelectorAll('button,input,select,summary')].filter(node=>!node.disabled&&!node.hidden&&node.getClientRects().length),first=list[0],last=list[list.length-1];if(event.shiftKey&&(document.activeElement===first||!root.contains(document.activeElement))){event.preventDefault();last?.focus();}else if(!event.shiftKey&&(document.activeElement===last||!root.contains(document.activeElement))){event.preventDefault();first?.focus();}event.stopImmediatePropagation();};
 document.addEventListener('keydown',s.keys,true);el('scan-close').onclick=closeScanner;el('scan-start').focus();

 function stopVideo(){
  s.generation++;s.stream?.getTracks().forEach(track=>track.stop());s.stream=null;clearTimeout(s.timer);s.torch=false;el('scan-torch').hidden=true;el('scan-zoom-wrap').hidden=true;el('scan-cover').hidden=false;el('scan-start').disabled=false;el('scan-start').innerHTML=`${ic('device')}${native?'Открыть камеру':'Включить камеру'}`;
 }

 function fillBarcode(result){
  if(result.spaceId)throw new Error('Это QR ЯРУС. Здесь нужен код с упаковки.');
  const form=$('#item-form');if(!form)throw new Error('Карточка товара уже закрыта.');
  if(result.kind==='item'&&result.id!==form.dataset.id)throw new Error('Этот код уже привязан к товару «'+result.entity.name+'».');
  form.elements.barcode.value=result.raw;form.elements.barcode.dispatchEvent(new Event('input',{bubbles:true}));
  const note=form.querySelector('#barcode-scan-note');if(note){note.hidden=false;note.textContent='Код добавлен. Сверьте цифры с упаковкой.';}
  modalDirty=true;closeScanner();form.elements.barcode.focus({preventScroll:true});
 }

 function accept(value,format=''){
  if(s.closed)return;stopVideo();
  try{
   const checked=YarusCodes.validateScan(value,format);value=checked.text;format=checked.format;
   if(target==='invite'){
    const invite=YarusCodes.parseInvite(value);if(invite.expires*1000<Date.now())throw new Error('Срок приглашения истёк. Создайте новое на компьютере.');closeScanner();accountModal('join',{server:invite.server,code:invite.code,transportKey:invite.transportKey});toast('Приглашение считано.');return;
   }
   const result=YarusCodes.resolve(state,value);
   if(target==='barcode'){fillBarcode(result);return;}
   if(target==='place'||target==='destination'){
    if(result.kind!=='place')throw new Error('Это не код места хранения.');const select=$(target==='destination'?'#op-destination':'#op-place');if(!select)throw new Error('Форма движения уже закрыта.');select.value=result.id;select.dispatchEvent(new Event('change',{bubbles:true}));modalDirty=true;closeScanner();return;
   }
   if(target==='item'){
    if(result.kind!=='item')throw new Error('Товар с таким кодом не найден. Сначала создайте карточку или привяжите код.');if(result.entity.archived)throw new Error('Товар находится в архиве.');const select=$('#op-item');if(!select)throw new Error('Форма движения уже закрыта.');select.value=result.id;select.dispatchEvent(new Event('change',{bubbles:true}));modalDirty=true;closeScanner();return;
   }
   if(result.kind==='item'){closeScanner();detailModal(result.id);return;}
   if(result.kind==='place'){closeScanner();closeModal(true);page='stock';placeFilter=result.id;search='';category='';stockFilter='all';render();toast('Место: '+result.entity.name);return;}
   status('Этот код ещё не привязан к товару.');
   el('scan-result').innerHTML=`<div class="notice scan-found"><strong>Код</strong><p class="scan-code">${esc(result.raw)}</p>${canWrite()?'<div class="scan-result-actions"><button class="btn primary" id="scan-create">Создать товар</button><button class="btn" id="scan-bind">Выбрать товар</button><button class="linkbtn" id="scan-again">Сканировать снова</button></div>':'Изменять каталог может сотрудник или владелец.'}</div>`;
   if(canWrite()){
    el('scan-create').onclick=()=>{const code=result.raw;closeScanner();newItemModal();const form=$('#item-form');form.elements.barcode.value=code;const note=form.querySelector('#barcode-scan-note');if(note){note.hidden=false;note.textContent='Код добавлен. Сверьте цифры с упаковкой.';}modalDirty=true;};
    el('scan-bind').onclick=()=>{const code=result.raw;closeScanner();bindCodeModal(code);};
    el('scan-again').onclick=()=>{clearResult();start();};
   }
  }catch(error){status(error.message||'Не удалось использовать этот код.',true);}
 }

 function confirmDetected(result){
  try{result=YarusCodes.validateScan(result.text,result.format);}catch(error){status(error.message,true);return;}
  stopVideo();status('Сверьте цифры с упаковкой.');
  el('scan-result').innerHTML=`<div class="notice scan-confirm"><strong>Код найден</strong><p class="scan-code">${esc(result.text)}</p><div class="scan-result-actions"><button class="btn primary" id="scan-use">Использовать</button><button class="btn" id="scan-repeat">Повторить</button></div></div>`;
  el('scan-use').onclick=()=>accept(result.text,result.format);el('scan-repeat').onclick=()=>{clearResult();start();};el('scan-use').focus();
 }

 function useDetected(result,alreadyConfirmed=false){
  const kind=YarusCodes.scanFormat(result.format);if(!alreadyConfirmed&&['EAN-13','EAN-8','Code 128'].includes(kind)){confirmDetected(result);return;}accept(result.text,kind);
 }

 async function optimiseTrack(track){
  let capabilities={};try{capabilities=track.getCapabilities?.()||{};}catch{}
  for(const [key,value] of [['focusMode','continuous'],['exposureMode','continuous'],['whiteBalanceMode','continuous']]){
   try{if(Array.isArray(capabilities[key])&&capabilities[key].includes(value))await track.applyConstraints({advanced:[{[key]:value}]});}catch{}
  }
  if(capabilities.torch){el('scan-torch').hidden=false;el('scan-torch').onclick=async()=>{try{s.torch=!s.torch;await track.applyConstraints({advanced:[{torch:s.torch}]});el('scan-torch').textContent=s.torch?'Выключить фонарик':'Фонарик';}catch{status('Фонарик недоступен.');}};}
  if(capabilities.zoom&&Number.isFinite(capabilities.zoom.min)&&Number.isFinite(capabilities.zoom.max)&&capabilities.zoom.max>capabilities.zoom.min){
   const input=el('scan-zoom'),output=el('scan-zoom-value'),current=track.getSettings?.().zoom||capabilities.zoom.min;input.min=capabilities.zoom.min;input.max=capabilities.zoom.max;input.step=capabilities.zoom.step||.1;input.value=current;output.value=Number(current).toFixed(1)+'×';el('scan-zoom-wrap').hidden=false;input.oninput=async()=>{output.value=Number(input.value).toFixed(1)+'×';try{await track.applyConstraints({advanced:[{zoom:Number(input.value)}]});}catch{}};
  }
 }

 function friendlyCamera(device,index,devices){
  const label=(device.label||'').toLowerCase(),front=/front|user|перед/.test(label),rear=/back|rear|environment|задн/.test(label);if(front)return 'Передняя камера';if(rear)return 'Задняя камера '+(devices.slice(0,index+1).filter(x=>/back|rear|environment|задн/.test((x.label||'').toLowerCase())).length||1);return 'Камера '+(index+1);
 }

 function centralDetection(list,video){
  const decoded=list.filter(item=>item?.rawValue);if(!decoded.length)return null;const width=video.videoWidth||1,height=video.videoHeight||1,inside=decoded.filter(item=>{const box=item.boundingBox;if(!box)return true;const x=box.x+box.width/2,y=box.y+box.height/2;return x>width*.1&&x<width*.9&&y>height*.14&&y<height*.86;});const pool=inside.length?inside:decoded;if(pool.length===1)return pool[0];return pool.sort((a,b)=>{const ac=a.boundingBox,bc=b.boundingBox;if(!ac)return 1;if(!bc)return-1;return Math.hypot(ac.x+ac.width/2-width/2,ac.y+ac.height/2-height/2)-Math.hypot(bc.x+bc.width/2-width/2,bc.y+bc.height/2-height/2);})[0];
 }

 async function start(deviceId=''){
  clearResult();
  if(native){
   if(s.nativePending)return;s.nativePending=true;el('scan-start').disabled=true;status('Открываем камеру…');try{AndroidFiles.scanCode(target);}catch(error){s.nativePending=false;el('scan-start').disabled=false;status('Не удалось открыть камеру телефона. Можно выбрать фото или ввести код.',true);}return;
  }
  stopVideo();const generation=s.generation;status('Включаем камеру…');el('scan-start').disabled=true;
  try{
   if(document.hidden)throw new Error('Вернитесь в ЯРУС и повторите.');
   if(!window.isSecureContext||!navigator.mediaDevices?.getUserMedia)throw new Error('Камера здесь недоступна. Используйте приложение, HTTPS, фото или ручной ввод.');
   const videoConstraints={width:{ideal:1920,max:1920},height:{ideal:1080,max:1080},frameRate:{ideal:30,max:30}};if(deviceId)videoConstraints.deviceId={exact:deviceId};else videoConstraints.facingMode={ideal:'environment'};
   const stream=await navigator.mediaDevices.getUserMedia({audio:false,video:videoConstraints});
   if(s.closed||s.generation!==generation){stream.getTracks().forEach(track=>track.stop());return;}
   s.stream=stream;const video=el('scan-video');video.srcObject=stream;await video.play();if(s.closed||s.generation!==generation)return;
   el('scan-cover').hidden=true;status('Держите код в рамке. Коснитесь изображения, если нужно навести резкость.');el('scan-start').disabled=false;el('scan-start').textContent='Остановить';
   const track=stream.getVideoTracks()[0];await optimiseTrack(track);
   try{const devices=(await navigator.mediaDevices.enumerateDevices()).filter(item=>item.kind==='videoinput');if(!s.closed&&devices.length>1){const select=el('scan-cameras');select.innerHTML=devices.map((device,index)=>`<option value="${esc(device.deviceId)}">${esc(friendlyCamera(device,index,devices))}</option>`).join('');select.value=track.getSettings().deviceId||'';el('scan-camera-choice').hidden=false;}}catch{}
   let detector=null;try{if(window.BarcodeDetector){const formats=await BarcodeDetector.getSupportedFormats(),allowed=formats.filter(format=>['qr_code','ean_13','ean_8','code_128'].includes(format));if(allowed.length)detector=new BarcodeDetector({formats:allowed});}}catch{}
   const canvas=document.createElement('canvas'),context=canvas.getContext('2d',{willReadFrequently:true});let last='',repeats=0,lastAt=0;
   async function tick(){
    if(s.closed||!s.stream||generation!==s.generation)return;
    try{
     let result=null;
     if(detector){try{const found=centralDetection(await detector.detect(video),video);if(found)result={text:found.rawValue,format:found.format};}catch{detector=null;}}
     if(!result&&video.videoWidth){const factor=Math.min(1,1280/Math.max(video.videoWidth,video.videoHeight));canvas.width=Math.max(40,Math.floor(video.videoWidth*factor/8)*8);canvas.height=Math.max(40,Math.floor(video.videoHeight*factor/8)*8);context.drawImage(video,0,0,canvas.width,canvas.height);result=YarusCodes.decode(context.getImageData(0,0,canvas.width,canvas.height));}
     if(result){const checked=YarusCodes.validateScan(result.text,result.format),now=Date.now();if(checked.text===last&&now-lastAt<2500)repeats++;else{last=checked.text;repeats=1;}lastAt=now;if(repeats>=3){useDetected(checked);return;}}
    }catch(error){if(error?.message)status(error.message,true);}
    if(!s.closed&&s.stream&&generation===s.generation)s.timer=setTimeout(tick,160);
   }
   tick();
  }catch(error){
   if(s.closed)return;stopVideo();const message=error.name==='NotAllowedError'?'Нет доступа к камере. Разрешите его в настройках или выберите фото.':error.name==='NotFoundError'?'Камера не найдена. Можно выбрать фото или ввести код.':error.name==='NotReadableError'?'Камера занята другим приложением. Закройте его и повторите.':error.message||'Не удалось включить камеру.';status(message,true);
  }
 }

 s.accept=accept;s.nativeDelivered=(value,format)=>{s.nativePending=false;el('scan-start').disabled=false;useDetected({text:value,format},true);};s.nativeCancelled=()=>{s.nativePending=false;closeScanner();};s.nativeFailed=message=>{s.nativePending=false;el('scan-start').disabled=false;status(message||'Не удалось открыть камеру. Выберите фото или введите код.',true);};s.stopVideo=stopVideo;
 el('scan-start').onclick=()=>s.stream?(stopVideo(),status('Камера выключена.')):start();el('scan-cameras').onchange=()=>start(el('scan-cameras').value);
 el('scan-manual-form').onsubmit=event=>{event.preventDefault();accept(el('scan-manual').value);};
 el('scan-image').onchange=async()=>{stopVideo();const file=el('scan-image').files[0];if(!file)return;try{if(file.size>15e6)throw new Error('Выберите изображение до 15 МБ.');const image=await readScanImage(file);if(s.closed)return;const canvas=document.createElement('canvas'),context=canvas.getContext('2d',{willReadFrequently:true}),factor=Math.min(1,1920/Math.max(image.naturalWidth,image.naturalHeight));canvas.width=Math.max(40,Math.floor(image.naturalWidth*factor/8)*8);canvas.height=Math.max(40,Math.floor(image.naturalHeight*factor/8)*8);context.drawImage(image,0,0,canvas.width,canvas.height);const result=YarusCodes.decode(context.getImageData(0,0,canvas.width,canvas.height));if(!result)throw new Error('Код не найден. Выберите чёткое фото без бликов.');useDetected(result);}catch(error){if(!s.closed)status(error.message||'Не удалось обработать изображение.',true);}finally{if(el('scan-image'))el('scan-image').value='';}};
 if(autoStart)start();
}

window.YarusNativeScanner={
 deliver(value,format){yarusScanSession?.nativeDelivered?.(String(value||''),String(format||''));},
 cancelled(){yarusScanSession?.nativeCancelled?.();},
 failed(message){yarusScanSession?.nativeFailed?.(String(message||''));}
};
function bindCodeModal(code){if(!canWrite())return;modal('Привязать код к товару','После подтверждения код будет записан в карточку.',`<form id="bind-code-form" class="stack"><div class="notice scan-code">${esc(code)}</div><label class="field">Найдите товар<input class="input" id="bind-search" placeholder="Название или артикул"></label><label class="field">Товар<select name="item" id="bind-item">${itemOptions('')}</select></label><p id="bind-hint" class="notice"></p><div class="notice warn">Остаток и история не изменятся. Уже существующий код будет заменён только после отдельного подтверждения.</div></form>`,formButtons('bind-code-form','Привязать код'));
 const select=$('#bind-item');function hint(){const x=state.items[select.value];$('#bind-hint').textContent=x?(x.barcode?'Текущий код: '+x.barcode:'Заводской код ещё не указан.'):'Нет подходящих товаров.';}$('#bind-search').oninput=e=>{const term=e.target.value.toLowerCase();select.innerHTML=activeItems().filter(x=>(x.name+' '+x.sku).toLowerCase().includes(term)).map(x=>`<option value="${x.id}">${esc(x.name)}</option>`).join('');hint();};select.onchange=hint;hint();$('#bind-code-form').onsubmit=e=>{e.preventDefault();const f=e.currentTarget;runForm(f,async()=>{const x=state.items[select.value];if(!x)throw new Error('Выберите товар.');if(x.barcode&&x.barcode!==code&&!confirm('Заменить код '+x.barcode+' у товара «'+x.name+'»?'))throw new Error('Код не изменён.');const r=await send({id:id(),type:'item',item:{...x,barcode:code}});toast(r.saved?'Код привязан.':'Привязка ожидает подтверждения сервера.');});};}
function upgradeOperation(kind){const f=$('#operation-form');if(!f)return;f.noValidate=true;for(const [select,target,label] of [['#op-item','item','Сканировать товар'],['#op-place','place','Сканировать место'],['#op-destination','destination','Сканировать место назначения']]){const el=$(select);if(el){const b=document.createElement('button');b.type='button';b.className='btn small scan-field-btn';b.innerHTML=ic('barcode')+label;b.onclick=()=>openScanner(target,true);el.parentElement.append(b);}}
 const input=$('#op-qty'),error=document.createElement('span');error.id='quantity-error';error.className='quantity-error';error.setAttribute('role','status');input.setAttribute('aria-describedby','qty-unit quantity-error');input.parentElement.append(error);
 function validate(){let message='';const item=state.items[f.elements.itemId.value],p=f.elements.place.value;try{if(input.value.trim()){const qty=D.decimal(input.value),available=state.stocks[D.key(item.id,p)]?.qty||0;if(kind!=='count'&&!qty)message='Количество должно быть больше нуля.';else if(['out','transfer'].includes(kind)&&qty>available)message=available?`В месте «${placeName(p)}» доступно только ${q(available)} ${item.unit}. Уменьшите количество или выберите другое место.`:`В месте «${placeName(p)}» товара нет. Выберите другое место или сначала оформите приход.`;else if(kind==='transfer'&&f.elements.destination.value===p)message='Выберите разные места: откуда и куда.';}}catch(e){message=e.message;}error.textContent=message;input.setAttribute('aria-invalid',message?'true':'false');return !message;}
 f.addEventListener('input',validate);f.addEventListener('change',validate);f.addEventListener('submit',e=>{if(!validate()){e.preventDefault();e.stopImmediatePropagation();error.scrollIntoView({block:'center',behavior:'smooth'});return;}if(!f.checkValidity()){e.preventDefault();e.stopImmediatePropagation();f.reportValidity();}},true);validate();}
function labelCanvas(payload,title,subtitle){const c=document.createElement('canvas');c.width=840;c.height=480;const x=c.getContext('2d');x.fillStyle='#fff';x.fillRect(0,0,c.width,c.height);const m=YarusCodes.matrix(payload),n=m.length+8,scale=Math.floor(360/n),offset=24;const top=Math.floor((c.height-n*scale)/2);x.fillStyle='#000';m.forEach((row,y)=>row.forEach((v,col)=>{if(v)x.fillRect(offset+(col+4)*scale,top+(y+4)*scale,scale,scale);}));const left=offset+n*scale+26,width=c.width-left-25;x.font='bold 36px sans-serif';x.fillText('ЯРУС',left,65);x.font='bold 40px sans-serif';const text=String(title);let lines=[],line='';for(const char of text){if(x.measureText(line+char).width>width){lines.push(line);line=char;}else line+=char;}if(line)lines.push(line);if(lines.length>4){lines=lines.slice(0,4);lines[3]=lines[3].slice(0,-2)+'…';}lines.forEach((l,i)=>x.fillText(l,left,118+i*46));x.font='28px sans-serif';const sub=String(subtitle);let s=sub;while(x.measureText(s).width>width&&s.length)s=s.slice(0,-1);if(s!==sub)s=s.slice(0,-1)+'…';x.fillText(s,left,365);x.font='22px sans-serif';x.fillText('Откройте сканером ЯРУС',left,418);return c;}
function labelsPDF(canvas,copies=1,a4=true){copies=Number(copies);if(!Number.isInteger(copies)||copies<1||copies>12)throw new Error('Выберите от 1 до 12 этикеток.');const jpeg=atob(canvas.toDataURL('image/jpeg',.98).split(',')[1]);let hex='';for(let i=0;i<jpeg.length;i++)hex+=jpeg.charCodeAt(i).toString(16).padStart(2,'0');hex+='>';const mm=72/25.4,w=a4?210*mm:70*mm,h=a4?297*mm:40*mm;let stream='';for(let i=0;i<(a4?copies:1);i++){const x=a4?(30+(i%2)*80)*mm:0,y=a4?h-(16+Math.floor(i/2)*45+40)*mm:0;stream+=`q ${70*mm} 0 0 ${40*mm} ${x} ${y} cm /Im0 Do Q\n`;}
 const objects=['<< /Type /Catalog /Pages 2 0 R >>','<< /Type /Pages /Kids [3 0 R] /Count 1 >>',`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${w} ${h}] /Resources << /XObject << /Im0 5 0 R >> >> /Contents 4 0 R >>`,`<< /Length ${stream.length} >>\nstream\n${stream}endstream`,`<< /Type /XObject /Subtype /Image /Width ${canvas.width} /Height ${canvas.height} /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter [/ASCIIHexDecode /DCTDecode] /Length ${hex.length} >>\nstream\n${hex}\nendstream`];let pdf='%PDF-1.4\n',offsets=[0];objects.forEach((o,i)=>{offsets.push(pdf.length);pdf+=(i+1)+' 0 obj\n'+o+'\nendobj\n';});const xref=pdf.length;pdf+='xref\n0 6\n0000000000 65535 f \n'+offsets.slice(1).map(o=>String(o).padStart(10,'0')+' 00000 n \n').join('')+'trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n'+xref+'\n%%EOF\n';return pdf;}
function labelModal(kind,entityID){try{const entity=(kind==='item'?state.items:state.places)[entityID];if(!entity)return;const payload=YarusCodes.internal(state.space.id,kind,entity.id);modal('QR-этикетка',kind==='item'?'Постоянный код товара. Остаток не записан в QR.':'Постоянный код места хранения.',`<div class="stack"><div class="label-preview" id="label-preview"></div><p class="notice">${esc(entity.name)}<br>Склад: ${esc(state.space.name)}. Код откроет карточку только в этом складе или его полной копии. Сам по себе код доступа не даёт.</p><div class="form-grid"><label class="field">Бумага<select id="label-paper"><option value="a4">Лист A4 · до 12 этикеток</option><option value="single">Одна этикетка · 70 × 40 мм</option></select></label><label class="field">Количество на A4<input class="input" id="label-copies" type="number" min="1" max="12" value="1"></label></div><button class="btn primary" id="label-pdf">${ic('download')}Сохранить PDF для печати</button><button class="btn" id="label-svg">Сохранить QR как SVG</button><p class="small muted">Печатайте PDF в масштабе 100% («Фактический размер»). Этикетка — 70 × 40 мм. Сначала проверьте одну наклейку камерой. Длинное название сокращается; полное — в карточке. Расположение на A4: две колонки, шесть рядов; не шаблон готовой самоклейки.</p><details><summary class="small">Текст кода для внешнего сканера</summary><p class="scan-code">${esc(payload)}</p></details></div>`);const canvas=labelCanvas(payload,entity.name,kind==='item'?(entity.sku||'Товар'):'Место хранения');$('#label-preview').append(canvas);$('#label-paper').onchange=()=>{$('#label-copies').disabled=$('#label-paper').value==='single';};$('#label-pdf').onclick=()=>{try{downloadText('YARUS-label-'+entity.id+'.pdf',labelsPDF(canvas,Number($('#label-copies').value),$('#label-paper').value==='a4'),'application/pdf');}catch(e){formError(e);}};$('#label-svg').onclick=()=>downloadText('YARUS-QR-'+entity.id+'.svg',YarusCodes.svg(payload),'image/svg+xml');}catch(e){toast(e.message,'error');}}
function pauseScanner(){const s=yarusScanSession;if(!s||s.nativePending)return;s.stopVideo?.();const status=s.root.querySelector('#scan-status');if(status)status.textContent='Камера выключена. Нажмите кнопку, чтобы продолжить.';}
document.addEventListener('visibilitychange',()=>{if(document.hidden)pauseScanner();});
