"""Validate PDFs produced by the actual client UI in codes_ui.py; render them."""
from pathlib import Path
import fitz, json, subprocess
from PIL import Image
R=Path(__file__).resolve().parents[1];reports=[]
for name,expected in [('label-a4-example.pdf',12),('label-70x40-example.pdf',1)]:
 doc=fitz.open(R/'docs'/name);assert len(doc)==1
 page=doc[0];images=page.get_images();assert len(images)==1
 boxes=page.get_image_rects(images[0][0]);assert len(boxes)==expected
 for box in boxes:
  assert page.rect.contains(box)
  assert abs(box.width*25.4/72-70)<0.01 and abs(box.height*25.4/72-40)<0.01
 page.get_pixmap(matrix=fitz.Matrix(2,2)).save(R/'docs'/(name[:-4]+'.png'))
 if expected==1:
  pix=page.get_pixmap(matrix=fitz.Matrix(3,3));im=Image.frombytes('RGB',(pix.width,pix.height),pix.samples).convert('RGBA')
  padded=Image.new('RGBA',((im.width+7)//8*8,(im.height+7)//8*8),'white');padded.paste(im,(0,0))
  path=R/'docs/pdf-label-render.rgba';path.write_bytes(padded.tobytes())
  js="""const fs=require('fs'),vm=require('vm');let c={};c.globalThis=c;for(const f of ['vendor/qr-decode.js','vendor/code128-patterns.js','codes.js'])vm.runInNewContext(fs.readFileSync('web/'+f,'utf8'),c);let b=fs.readFileSync('docs/pdf-label-render.rgba');let result=c.YarusCodes.decode({data:new Uint8ClampedArray(b),width:WIDTH,height:HEIGHT});if(!result||!result.text.startsWith('YARUS:1:')||!result.text.endsWith(':item:demo-item-2'))throw Error('PDF QR not readable');console.log('PASS: QR decoded from actual rasterized PDF: '+result.format);""".replace('WIDTH',str(padded.width)).replace('HEIGHT',str(padded.height))
  result=subprocess.run(['node','-e',js],cwd=R,check=True,capture_output=True,text=True);reports.append(result.stdout.strip());path.unlink()
 reports.append(f'PASS: {name}: {expected} label placement(s), exact 70x40 mm, within page; PDF rasterized')
text='\n'.join(reports)+'\nNOT TESTED: physical printer, adhesive stock, real camera reading paper.\n';(R/'docs/pdf-tests.txt').write_text(text);print(text)
