/* ЯРУС 1.0 - printable stock reports generated entirely on the device. */
(function(root){
 'use strict';
 const PAGE_WIDTH=1240,PAGE_HEIGHT=1754,PDF_WIDTH=595.28,PDF_HEIGHT=841.89;
 const ROWS_PER_PAGE=36,DETAIL_LIMIT=5000;

 function qty(value){return new Intl.NumberFormat('ru-RU',{maximumFractionDigits:3}).format(value/1000);}
 function money(value,currency){let cents=typeof value==='bigint'?value:BigInt(Math.round(Number(value)||0)),negative=cents<0n;if(negative)cents=-cents;const units=(cents+50n)/100n,symbol={RUB:'₽',EUR:'€',USD:'$'}[currency]||currency;return `${negative?'−':''}${new Intl.NumberFormat('ru-RU').format(units)} ${symbol}`;}
 function stockValue(quantity,price){return (BigInt(quantity)*BigInt(price)+500n)/1000n;}
 function total(state,itemId,placeId=''){let value=0;for(const stock of Object.values(state.stocks||{}))if(stock.item===itemId&&(!placeId||stock.place===placeId))value+=stock.qty;return value;}
 function fit(ctx,value,width){let text=String(value??'');if(ctx.measureText(text).width<=width)return text;while(text.length>1&&ctx.measureText(text+'...').width>width)text=text.slice(0,-1);return text+'...';}

 function rowsFor(state,options){
  const allowed=options.itemIds?new Set(options.itemIds):null;
  const items=Object.values(state.items||{}).filter(item=>!item.archived&&(!allowed||allowed.has(item.id)));
  if(options.layout==='places'){
   const rows=[];
   for(const stock of Object.values(state.stocks||{})){
    const item=state.items[stock.item],place=state.places[stock.place];
    if(!item||item.archived||!place||stock.qty===0||(allowed&&!allowed.has(item.id))||(options.placeId&&stock.place!==options.placeId))continue;
    rows.push({place:place.name,name:item.name,sku:item.sku||'',quantity:qty(stock.qty)+' '+item.unit,value:stockValue(stock.qty,item.price)});
   }
   rows.sort((a,b)=>a.place.localeCompare(b.place,'ru')||a.name.localeCompare(b.name,'ru'));
   if(rows.length>DETAIL_LIMIT)throw new Error('В подробном отчёте больше 5000 строк. Уточните место или фильтр либо используйте CSV.');
   return rows;
  }
  return items.sort((a,b)=>a.name.localeCompare(b.name,'ru')).map(item=>{
   const amount=total(state,item.id,options.placeId||'');
   return {name:item.name,sku:item.sku||'',quantity:qty(amount)+' '+item.unit,minimum:qty(item.min)+' '+item.unit,value:stockValue(amount,item.price)};
  });
 }

 function drawHeader(ctx,state,options,rowCount,totalValue){
  ctx.fillStyle='#ffffff';ctx.fillRect(0,0,PAGE_WIDTH,PAGE_HEIGHT);
  ctx.fillStyle='#153f3b';ctx.fillRect(0,0,PAGE_WIDTH,18);
  ctx.fillStyle='#153f3b';ctx.font='700 42px Arial, sans-serif';ctx.fillText('ЯРУС',70,90);
  ctx.fillStyle='#1c2c2b';ctx.font='700 32px Arial, sans-serif';ctx.fillText(options.layout==='places'?'Остатки по местам хранения':'Сводный отчёт об остатках',70,145);
  ctx.fillStyle='#697771';ctx.font='20px Arial, sans-serif';ctx.fillText(fit(ctx,state.space.name,700),70,185);
  const created=new Intl.DateTimeFormat('ru-RU',{dateStyle:'medium',timeStyle:'short'}).format(new Date());
  ctx.textAlign='right';ctx.fillText('Сформировано: '+created,PAGE_WIDTH-70,90);
  ctx.fillText('Позиций: '+rowCount,PAGE_WIDTH-70,130);
  ctx.fillText('Справочная оценка: '+money(totalValue,state.space.currency),PAGE_WIDTH-70,170);ctx.textAlign='left';
  ctx.fillStyle='#e9f3ed';ctx.fillRect(70,205,PAGE_WIDTH-140,48);
  ctx.fillStyle='#31594f';ctx.font='18px Arial, sans-serif';
  const place=options.placeId?state.places[options.placeId]?.name:'Все места';
  ctx.fillText(fit(ctx,'Фильтр: '+(options.scopeLabel||'Все активные товары')+' · '+(place||'Все места'),PAGE_WIDTH-180),90,236);
 }

 function drawPage(state,options,rows,pageIndex,pageCount,totalValue){
  const canvas=document.createElement('canvas');canvas.width=PAGE_WIDTH;canvas.height=PAGE_HEIGHT;
  const ctx=canvas.getContext('2d');drawHeader(ctx,state,options,rows.length,totalValue);
  const top=285,rowHeight=38,pageRows=rows.slice(pageIndex*ROWS_PER_PAGE,(pageIndex+1)*ROWS_PER_PAGE);
  ctx.fillStyle='#153f3b';ctx.fillRect(70,top,PAGE_WIDTH-140,46);ctx.fillStyle='#ffffff';ctx.font='700 17px Arial, sans-serif';
  if(options.layout==='places'){
   ctx.fillText('Место',86,top+29);ctx.fillText('Товар',345,top+29);ctx.fillText('Артикул',790,top+29);ctx.textAlign='right';ctx.fillText('Остаток',PAGE_WIDTH-86,top+29);ctx.textAlign='left';
  }else{
   ctx.fillText('Товар',86,top+29);ctx.fillText('Артикул',535,top+29);ctx.textAlign='right';ctx.fillText('Остаток',830,top+29);ctx.fillText('Минимум',1000,top+29);ctx.fillText('Оценка',PAGE_WIDTH-86,top+29);ctx.textAlign='left';
  }
  ctx.font='18px Arial, sans-serif';
  pageRows.forEach((row,index)=>{
   const y=top+46+index*rowHeight;if(index%2===0){ctx.fillStyle='#f5f6f3';ctx.fillRect(70,y,PAGE_WIDTH-140,rowHeight);}ctx.fillStyle='#1c2c2b';
   if(options.layout==='places'){
    ctx.fillText(fit(ctx,row.place,235),86,y+26);ctx.fillText(fit(ctx,row.name,420),345,y+26);ctx.fillStyle='#697771';ctx.fillText(fit(ctx,row.sku,150),790,y+26);ctx.fillStyle='#1c2c2b';ctx.textAlign='right';ctx.fillText(row.quantity,PAGE_WIDTH-86,y+26);ctx.textAlign='left';
   }else{
    ctx.fillText(fit(ctx,row.name,425),86,y+26);ctx.fillStyle='#697771';ctx.fillText(fit(ctx,row.sku,180),535,y+26);ctx.fillStyle='#1c2c2b';ctx.textAlign='right';ctx.fillText(row.quantity,830,y+26);ctx.fillText(row.minimum,1000,y+26);ctx.fillText(money(row.value,state.space.currency),PAGE_WIDTH-86,y+26);ctx.textAlign='left';
   }
   ctx.strokeStyle='#e2e8e1';ctx.beginPath();ctx.moveTo(70,y+rowHeight);ctx.lineTo(PAGE_WIDTH-70,y+rowHeight);ctx.stroke();
  });
  if(!rows.length){ctx.fillStyle='#697771';ctx.font='24px Arial, sans-serif';ctx.fillText('По выбранным условиям позиций нет.',86,top+100);}
  ctx.fillStyle='#697771';ctx.font='16px Arial, sans-serif';ctx.fillText('Справочная оценка не является бухгалтерской себестоимостью.',70,PAGE_HEIGHT-58);ctx.textAlign='right';ctx.fillText('Страница '+(pageIndex+1)+' из '+pageCount,PAGE_WIDTH-70,PAGE_HEIGHT-58);ctx.textAlign='left';
  const data=canvas.toDataURL('image/jpeg',0.88).split(',')[1];return {width:canvas.width,height:canvas.height,jpeg:atob(data)};
 }

 function ascii85(binary){let out='';for(let i=0;i<binary.length;i+=4){const size=Math.min(4,binary.length-i),bytes=[0,0,0,0];for(let j=0;j<size;j++)bytes[j]=binary.charCodeAt(i+j);let value=((bytes[0]*256+bytes[1])*256+bytes[2])*256+bytes[3];if(value===0&&size===4){out+='z';continue;}const chars=['!','!','!','!','!'];for(let j=4;j>=0;j--){chars[j]=String.fromCharCode(value%85+33);value=Math.floor(value/85);}out+=chars.slice(0,size+1).join('');}return out+'~>';}

 function pdfFromImages(images){
  const kids=images.map((_,index)=>(3+index*3)+' 0 R').join(' '),objects=[`<< /Type /Catalog /Pages 2 0 R >>`,`<< /Type /Pages /Kids [${kids}] /Count ${images.length} >>`];
  images.forEach((image,index)=>{const page=3+index*3,content=page+1,picture=page+2,name='Im'+index,stream=`q ${PDF_WIDTH} 0 0 ${PDF_HEIGHT} 0 0 cm /${name} Do Q\n`,encoded=ascii85(image.jpeg);objects.push(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${PDF_WIDTH} ${PDF_HEIGHT}] /Resources << /XObject << /${name} ${picture} 0 R >> >> /Contents ${content} 0 R >>`,`<< /Length ${stream.length} >>\nstream\n${stream}endstream`,`<< /Type /XObject /Subtype /Image /Width ${image.width} /Height ${image.height} /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter [/ASCII85Decode /DCTDecode] /Length ${encoded.length} >>\nstream\n${encoded}\nendstream`);});
  let pdf='%PDF-1.4\n',offsets=[0];objects.forEach((object,index)=>{offsets.push(pdf.length);pdf+=(index+1)+' 0 obj\n'+object+'\nendobj\n';});const xref=pdf.length;pdf+='xref\n0 '+(objects.length+1)+'\n0000000000 65535 f \n'+offsets.slice(1).map(offset=>String(offset).padStart(10,'0')+' 00000 n \n').join('')+'trailer\n<< /Size '+(objects.length+1)+' /Root 1 0 R >>\nstartxref\n'+xref+'\n%%EOF\n';return pdf;
 }

 async function stockPDF(state,options={}){
  const rows=rowsFor(state,options),pageCount=Math.max(1,Math.ceil(rows.length/ROWS_PER_PAGE)),totalValue=rows.reduce((sum,row)=>sum+(row.value||0n),0n),images=[];
  for(let page=0;page<pageCount;page++){images.push(drawPage(state,options,rows,page,pageCount,totalValue));options.onProgress?.(page+1,pageCount);if(page+1<pageCount)await new Promise(resolve=>setTimeout(resolve,0));}
  return pdfFromImages(images);
 }

 root.YarusReports={stockPDF,rowsFor,pdfFromImages,ascii85};
})(globalThis);
