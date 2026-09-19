#!/usr/bin/env python3
"""Offline experimental APK builder. Standard Android DEX/AXML/resources/APK v2.
No SDK is distributed. Native launch MUST be checked on a real device.
Builds the small WebView host described in android/app/src/main/java.
"""
from pathlib import Path
import os, struct as st, hashlib, zlib, zipfile, io, base64, datetime, json
from cryptography import x509
from cryptography.x509.oid import NameOID
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa,padding
ROOT=Path(__file__).resolve().parents[1]; OUT=ROOT/'dist'; OUT.mkdir(exist_ok=True)
def u16(x):return st.pack('<H',x&65535)
def u32(x):return st.pack('<I',x&0xffffffff)
def u64(x):return st.pack('<Q',x)
def U(n):
 b=bytearray()
 while True:
  v=n&127;n>>=7;b.append(v|(128 if n else 0))
  if not n:return bytes(b)
def S(n):
 b=bytearray()
 while True:
  v=n&127;n>>=7;done=(n==0 and not v&64) or (n==-1 and v&64);b.append(v|(0 if done else 128))
  if done:return bytes(b)
def LP(b):return u32(len(b))+b
class Dex:
 def __init__(self):self.strings=set();self.types=set();self.protos=set();self.methods=set();self.fields=set();self.classes=[]
 def string(self,s):self.strings.add(s);return s
 def typ(self,t):self.types.add(t);self.string(t);return t
 def proto(self,r,args):
  self.typ(r)
  for a in args:self.typ(a)
  short=lambda t:'L' if t[0] in '[L' else t
  self.string(short(r)+''.join(map(short,args)));p=(r,tuple(args));self.protos.add(p);return p
 def method(self,c,n,r='V',args=()):
  self.typ(c);self.string(n);p=self.proto(r,args);m=(c,n,p);self.methods.add(m);return m
 def field(self,c,n,t):self.typ(c);self.typ(t);self.string(n);f=(c,n,t);self.fields.add(f);return f
 def clazz(self,t,s='Ljava/lang/Object;',interfaces=()):
  self.typ(t);self.typ(s)
  for i in interfaces:self.typ(i)
  c={'type':t,'super':s,'interfaces':interfaces,'methods':[],'fields':[]};self.classes.append(c);return c
 def code(self,c,n,r='V',args=(),regs=8,flags=1,annot=False):
  m=self.method(c['type'],n,r,args);a=Asm(self);c['methods'].append((m,flags,regs,len(args)+1,a,annot));return a
 def build(self):
  self.typ('Ljava/lang/Exception;');self.typ('Landroid/webkit/JavascriptInterface;')
  self.ss=sorted(self.strings,key=lambda s:s.encode('utf-16-be'));self.si={s:i for i,s in enumerate(self.ss)}
  self.tt=sorted(self.types,key=lambda t:self.si[t]);self.ti={t:i for i,t in enumerate(self.tt)}
  self.pp=sorted(self.protos,key=lambda p:(self.ti[p[0]],tuple(self.ti[t] for t in p[1])));self.pi={p:i for i,p in enumerate(self.pp)}
  self.ff=sorted(self.fields,key=lambda f:(self.ti[f[0]],self.si[f[1]],self.ti[f[2]]));self.fi={f:i for i,f in enumerate(self.ff)}
  self.mm=sorted(self.methods,key=lambda m:(self.ti[m[0]],self.si[m[1]],self.pi[m[2]]));self.mi={m:i for i,m in enumerate(self.mm)}
  classes=sorted(self.classes,key=lambda c:self.ti[c['type']]);off=112;fixed={}
  for name,seq,width in [('strings',self.ss,4),('types',self.tt,4),('protos',self.pp,12),('fields',self.ff,8),('methods',self.mm,8),('classes',classes,32)]:fixed[name]=off;off+=len(seq)*width
  dataoff=(off+3)&~3;b=bytearray(dataoff);maps=[(0,1,0),(1,len(self.ss),fixed['strings']),(2,len(self.tt),fixed['types']),(3,len(self.pp),fixed['protos']),(4,len(self.ff),fixed['fields']),(5,len(self.mm),fixed['methods']),(6,len(classes),fixed['classes'])]
  def align(n=4):b.extend(b'\0'*((-len(b))%n))
  def section(kind,count):align();maps.append((kind,count,len(b)))
  stroffs={};section(0x2002,len(self.ss))
  for s in self.ss:
   stroffs[s]=len(b);utf=s.encode('utf-16-be');units=[int.from_bytes(utf[i:i+2],'big') for i in range(0,len(utf),2)];encoded=bytearray()
   for c in units:
    if 0<c<128:encoded.append(c)
    elif c<2048:encoded.extend([192|(c>>6),128|(c&63)])
    else:encoded.extend([224|(c>>12),128|((c>>6)&63),128|(c&63)])
   b.extend(U(len(units))+encoded+b'\0')
  lists=set(p[1] for p in self.pp if p[1])|set(tuple(c['interfaces']) for c in classes if c['interfaces']);tl={}
  if lists:
   section(0x1001,len(lists))
   for ts in sorted(lists):
    align();tl[ts]=len(b);b.extend(u32(len(ts))+b''.join(u16(self.ti[t]) for t in ts))
  # One runtime annotation instance shared by both bridge methods.
  section(0x2004,1);ann=len(b);b.extend(bytes([1])+U(self.ti['Landroid/webkit/JavascriptInterface;'])+U(0))
  section(0x1003,1);annset=len(b);b.extend(u32(1)+u32(ann))
  ac=[c for c in classes if any(m[5] for m in c['methods'])];annoff={}
  if ac:
   section(0x2006,len(ac))
   for c in ac:
    annotated=sorted([self.mi[m[0]] for m in c['methods'] if m[5]]);annoff[c['type']]=len(b);b.extend(u32(0)+u32(0)+u32(len(annotated))+u32(0))
    for mi in annotated:b.extend(u32(mi)+u32(annset))
  methall=[m for c in classes for m in c['methods']];codeoffs={};section(0x2001,len(methall))
  for m,flags,regs,ins,a,annot in methall:
   # ART rejects outs_size > 5 when it also exceeds registers_size.
   # Reserve precisely the maximum outgoing argument-word count, not a
   # global minimum of six. This is essential for small constructors.
   assert ins <= regs, (m, ins, regs)
   assert a.maxouts <= 5 or a.maxouts <= regs, (m, a.maxouts, regs)
   align();codeoffs[m]=len(b);code,labels=a.encode(self);tries=a.tries;b.extend(u16(regs)+u16(ins)+u16(a.maxouts)+u16(len(tries))+u32(0)+u32(len(code)));b.extend(b''.join(map(u16,code)))
   if tries:
    if len(code)%2:b.extend(u16(0))
    handlers=bytearray(U(len(tries)));handleroffs=[]
    for start,end,target in tries:
     handleroffs.append(len(handlers));handlers.extend(S(1)+U(self.ti['Ljava/lang/Exception;'])+U(labels[target]))
    for (start,end,target),ho in zip(tries,handleroffs):b.extend(u32(labels[start])+u16(labels[end]-labels[start])+u16(ho))
    b.extend(handlers)
  section(0x2000,len(classes));classoffs={}
  for c in classes:
   classoffs[c['type']]=len(b);fields=sorted(c['fields'],key=lambda f:self.fi[f]);direct=sorted([m for m in c['methods'] if m[0][1]=='<init>'],key=lambda m:self.mi[m[0]]);virtual=sorted([m for m in c['methods'] if m[0][1]!='<init>'],key=lambda m:self.mi[m[0]])
   b.extend(U(0)+U(len(fields))+U(len(direct))+U(len(virtual)));last=0
   for f in fields:i=self.fi[f];b.extend(U(i-last)+U(1));last=i
   for methods in [direct,virtual]:
    last=0
    for m,flags,*_ in methods:i=self.mi[m];b.extend(U(i-last)+U(flags)+U(codeoffs[m]));last=i
  align();mapoff=len(b);maps.append((0x1000,1,mapoff));maps=sorted([x for x in maps if x[1]],key=lambda x:x[2]);b.extend(u32(len(maps)))
  for typ,count,o in maps:b.extend(u16(typ)+u16(0)+u32(count)+u32(o))
  for i,s in enumerate(self.ss):st.pack_into('<I',b,fixed['strings']+4*i,stroffs[s])
  for i,t in enumerate(self.tt):st.pack_into('<I',b,fixed['types']+4*i,self.si[t])
  for i,p in enumerate(self.pp):
   short=lambda t:'L' if t[0] in '[L' else t
   st.pack_into('<III',b,fixed['protos']+12*i,self.si[short(p[0])+''.join(map(short,p[1]))],self.ti[p[0]],tl.get(p[1],0))
  for i,f in enumerate(self.ff):st.pack_into('<HHI',b,fixed['fields']+8*i,self.ti[f[0]],self.ti[f[2]],self.si[f[1]])
  for i,m in enumerate(self.mm):st.pack_into('<HHI',b,fixed['methods']+8*i,self.ti[m[0]],self.pi[m[2]],self.si[m[1]])
  for i,c in enumerate(classes):st.pack_into('<IIIIIIII',b,fixed['classes']+32*i,self.ti[c['type']],1,self.ti[c['super']],tl.get(tuple(c['interfaces']),0),0xffffffff,annoff.get(c['type'],0),classoffs[c['type']],0)
  b[:8]=b'dex\n035\0';st.pack_into('<20I',b,32,len(b),112,0x12345678,0,0,mapoff,len(self.ss),fixed['strings'],len(self.tt),fixed['types'],len(self.pp),fixed['protos'],len(self.ff),fixed['fields'],len(self.mm),fixed['methods'],len(classes),fixed['classes'],len(b)-dataoff,dataoff)
  b[12:32]=hashlib.sha1(b[32:]).digest();st.pack_into('<I',b,8,zlib.adler32(b[12:]));return bytes(b)
class Asm:
 def __init__(self,d):self.d=d;self.ops=[];self.tries=[];self.maxouts=0
 def emit(self,*u):self.ops.append(('raw',list(u)));return self
 def label(self,n):self.ops.append(('label',n));return self
 def const(self,r,n):return self.emit(0x14|(r<<8),n&65535,(n>>16)&65535)
 def string(self,r,s):self.d.string(s);self.ops.append(('string',r,s));return self
 def new(self,r,t):self.d.typ(t);self.ops.append(('new',r,t));return self
 def array(self,r,size,t):self.d.typ(t);self.ops.append(('array',r,size,t));return self
 def mov(self,a,b):return self.emit(0x07|(a<<8)|(b<<12))
 def result(self,r,obj=True):return self.emit((0x0c if obj else 0x0a)|(r<<8))
 def get(self,r,o,f):self.ops.append(('field',0x54,r,o,f));return self
 def put(self,r,o,f):self.ops.append(('field',0x5b,r,o,f));return self
 def invoke(self,kind,m,regs):
  self.maxouts=max(self.maxouts,len(regs));self.ops.append(('invoke',kind,m,list(regs)));return self
 def branch(self,kind,a,b,target):self.ops.append(('branch',kind,a,b,target));return self
 def go(self,t):self.ops.append(('goto',t));return self
 def ret(self):return self.emit(0x0e)
 def encode(self,d):
  labels={};pos=0
  sizes={'raw':lambda o:len(o[1]),'label':lambda o:0,'invoke':lambda o:3,'string':lambda o:2,'new':lambda o:2,'array':lambda o:2,'field':lambda o:2,'branch':lambda o:2,'goto':lambda o:2}
  for o in self.ops:
   if o[0]=='label':labels[o[1]]=pos
   pos+=sizes[o[0]](o)
  out=[]
  for o in self.ops:
   typ=o[0];p=len(out)
   if typ=='raw':out+=o[1]
   elif typ=='label':continue
   elif typ=='string':out +=[0x1a|(o[1]<<8),d.si[o[2]]]
   elif typ=='new':out +=[0x22|(o[1]<<8),d.ti[o[2]]]
   elif typ=='array':out +=[0x23|(o[1]<<8)|(o[2]<<12),d.ti[o[3]]]
   elif typ=='field':out +=[o[1]|(o[2]<<8)|(o[3]<<12),d.fi[o[4]]]
   elif typ=='invoke':
    _,kind,m,rs=o;op={'virtual':0x6e,'super':0x6f,'direct':0x70,'static':0x71,'interface':0x72,'range':0x74}[kind]
    if kind=='range':assert rs==list(range(rs[0],rs[0]+len(rs)));out +=[op|(len(rs)<<8),d.mi[m],rs[0]]
    else:
     assert len(rs)<=5 and all(r<16 for r in rs);padded=rs+[0]*(5-len(rs));c,e,f,g,h=padded;out +=[op|(len(rs)<<12)|(h<<8),d.mi[m],c|(e<<4)|(f<<8)|(g<<12)]
   elif typ=='branch':
    _,kind,a,b,target=o;op={'eq':0x32,'ne':0x33,'eqz':0x38,'nez':0x39}[kind];out +=[op|(a<<8)|((b<<12) if b is not None else 0),(labels[target]-p)&65535]
   elif typ=='goto':out +=[0x29,(labels[o[1]]-p)&65535]
  return out,labels
D=Dex();M='Lru/viktortown/yarus/MainActivity;';G='Lru/viktortown/yarus/GuardClient;';C='Lru/viktortown/yarus/ChromeClient;';B='Lru/viktortown/yarus/Bridge;';T='Lru/viktortown/yarus/ExportTask;'
ACT='Landroid/app/Activity;';OBJ='Ljava/lang/Object;';STR='Ljava/lang/String;';CTX='Landroid/content/Context;';W='Landroid/webkit/WebView;';WC='Landroid/webkit/WebChromeClient;';WV='Landroid/webkit/WebViewClient;';SET='Landroid/webkit/WebSettings;';INT='Landroid/content/Intent;';VC='Landroid/webkit/ValueCallback;';URI='Landroid/net/Uri;';TOAST='Landroid/widget/Toast;';RUN='Ljava/lang/Runnable;';FC='Landroid/webkit/WebChromeClient$FileChooserParams;'
mc=D.clazz(M,ACT);gc=D.clazz(G,WV);cc=D.clazz(C,WC);bc=D.clazz(B);tc=D.clazz(T,interfaces=(RUN,))
fw=D.field(M,'web',W);fp=D.field(M,'pendingText',STR);fc=D.field(M,'fileCallback',VC);mc['fields']=[fw,fp,fc]
ca=D.field(C,'activity',M);cc['fields']=[ca];ba=D.field(B,'activity',M);bc['fields']=[ba]
ta=D.field(T,'activity',M);tn=D.field(T,'name',STR);tt=D.field(T,'text',STR);tm=D.field(T,'mime',STR);tc['fields']=[ta,tn,tt,tm]
method=D.method
for clazz,sup in [(mc,ACT),(gc,WV)]:
 a=D.code(clazz,'<init>',regs=1,flags=0x10001);a.invoke('direct',method(sup,'<init>'),[0]).ret()
for clazz,field,sup in [(cc,ca,WC),(bc,ba,OBJ)]:
 a=D.code(clazz,'<init>',args=(M,),regs=2,flags=0x10001);a.invoke('direct',method(sup,'<init>'),[0]).put(1,0,field).ret()
a=D.code(tc,'<init>',args=(M,STR,STR,STR),regs=5,flags=0x10001);a.invoke('direct',method(OBJ,'<init>'),[0])
for r,f in [(1,ta),(2,tn),(3,tt),(4,tm)]:a.put(r,0,f)
a.ret()
# Lifecycle and local assets.
a=D.code(mc,'onCreate',args=('Landroid/os/Bundle;',),regs=16,flags=4);a.mov(0,14).invoke('super',method(ACT,'onCreate',args=('Landroid/os/Bundle;',)),[0,15]).label('start')
a.const(2,1).invoke('virtual',method(ACT,'requestWindowFeature','Z',('I',)),[0,2])
a.invoke('virtual',method(ACT,'getWindow','Landroid/view/Window;'),[0]).result(3).const(2,0xff153f3b)
a.invoke('virtual',method('Landroid/view/Window;','setStatusBarColor',args=('I',)),[3,2]).invoke('virtual',method('Landroid/view/Window;','setNavigationBarColor',args=('I',)),[3,2])
a.new(1,W).invoke('direct',method(W,'<init>',args=(CTX,)),[1,0]).put(1,0,fw)
a.invoke('virtual',method(W,'getSettings',SET),[1]).result(2).const(3,1)
for n in ['setJavaScriptEnabled','setDomStorageEnabled','setAllowContentAccess']:a.invoke('virtual',method(SET,n,args=('Z',)),[2,3])
a.const(3,0)
for n in ['setAllowFileAccess','setMediaPlaybackRequiresUserGesture']:a.invoke('virtual',method(SET,n,args=('Z',)),[2,3])
a.invoke('virtual',method(SET,'setMixedContentMode',args=('I',)),[2,3])
a.new(3,G).invoke('direct',method(G,'<init>'),[3]).invoke('virtual',method(W,'setWebViewClient',args=(WV,)),[1,3])
a.new(3,C).invoke('direct',method(C,'<init>',args=(M,)),[3,0]).invoke('virtual',method(W,'setWebChromeClient',args=(WC,)),[1,3])
a.new(3,B).invoke('direct',method(B,'<init>',args=(M,)),[3,0]).string(4,'AndroidFiles').invoke('virtual',method(W,'addJavascriptInterface',args=(OBJ,STR)),[1,3,4])
a.invoke('virtual',method(ACT,'setContentView',args=('Landroid/view/View;',)),[0,1])
a.invoke('virtual',method(CTX,'getAssets','Landroid/content/res/AssetManager;'),[0]).result(2).string(3,'index.html').invoke('virtual',method('Landroid/content/res/AssetManager;','open','Ljava/io/InputStream;',(STR,)),[2,3]).result(2)
a.new(3,'Ljava/util/Scanner;').string(4,'UTF-8').invoke('direct',method('Ljava/util/Scanner;','<init>',args=('Ljava/io/InputStream;',STR)),[3,2,4]).string(4,'\\A').invoke('virtual',method('Ljava/util/Scanner;','useDelimiter','Ljava/util/Scanner;',(STR,)),[3,4])
a.invoke('virtual',method('Ljava/util/Scanner;','next',STR),[3]).result(7).invoke('virtual',method('Ljava/util/Scanner;','close'),[3])
a.mov(5,1).string(6,'https://appassets.androidplatform.net/').string(8,'text/html').string(9,'UTF-8').const(10,0).invoke('range',method(W,'loadDataWithBaseURL',args=(STR,STR,STR,STR,STR)),list(range(5,11))).label('end').ret()
a.label('failure').emit(0x0d|(1<<8)).string(1,'Не удалось открыть ЯРУС. Обновите Android System WebView.').const(2,1).invoke('static',method(TOAST,'makeText',TOAST,(CTX,'Ljava/lang/CharSequence;','I')),[0,1,2]).result(1).invoke('virtual',method(TOAST,'show'),[1]).invoke('virtual',method(ACT,'finish'),[0]).ret();a.tries=[('start','end','failure')]
# Never navigate the trusted bridge-equipped WebView to any external page.
for args in [(W,STR),(W,'Landroid/webkit/WebResourceRequest;')]:
 a=D.code(gc,'shouldOverrideUrlLoading','Z',args,regs=4);a.const(0,1).emit(0x0f)
a=D.code(mc,'onBackPressed',regs=4);a.get(0,3,fw).branch('eqz',0,None,'default').string(1,"if(typeof yarusScanSession!=='undefined'&&yarusScanSession){closeScanner();}else if(document.querySelector('#modal-root .modal')){closeModal();}else if(typeof page==='string'&&page!=='home'&&state){page='home';render();}else{AndroidFiles.closeApp();}").const(2,0).invoke('virtual',method(W,'evaluateJavascript',args=(STR,VC)),[0,1,2]).ret().label('default').invoke('super',method(ACT,'onBackPressed'),[3]).ret()
# onDestroy is emitted below after camera fields are declared.
# SAF chooser, no filesystem/storage permission.
a=D.code(cc,'onShowFileChooser','Z',(W,VC,FC),regs=7);a.get(0,3,ca).get(1,0,fc).branch('eqz',1,None,'none').const(2,0).invoke('interface',method(VC,'onReceiveValue',args=(OBJ,)),[1,2]).label('none').put(5,0,fc)
a.new(1,INT).string(2,'android.intent.action.GET_CONTENT').invoke('direct',method(INT,'<init>',args=(STR,)),[1,2]).string(2,'android.intent.category.OPENABLE').invoke('virtual',method(INT,'addCategory',INT,(STR,)),[1,2]).string(2,'*/*').invoke('virtual',method(INT,'setType',INT,(STR,)),[1,2]).const(2,1002).invoke('virtual',method(ACT,'startActivityForResult',args=(INT,'I')),[0,1,2]).const(0,1).emit(0x0f)
# Bridge marshals actions to the UI thread; exported names come from app code.
a=D.code(bc,'saveText',args=(STR,STR,STR),regs=8,annot=True);a.get(0,4,ba).new(1,T).invoke('direct',method(T,'<init>',args=(M,STR,STR,STR)),[1,0,5,6,7]).invoke('virtual',method(ACT,'runOnUiThread',args=(RUN,)),[0,1]).ret()
a=D.code(bc,'closeApp',regs=4,annot=True);a.get(0,3,ba).new(1,T).const(2,0).invoke('direct',method(T,'<init>',args=(M,STR,STR,STR)),[1,0,2,2,2]).invoke('virtual',method(ACT,'runOnUiThread',args=(RUN,)),[0,1]).ret()
a=D.code(tc,'run',regs=6);a.get(0,5,ta).get(1,5,tn).branch('nez',1,None,'export').invoke('virtual',method(ACT,'finish'),[0]).ret().label('export').label('start').get(2,5,tt).put(2,0,fp).new(2,INT).string(3,'android.intent.action.CREATE_DOCUMENT').invoke('direct',method(INT,'<init>',args=(STR,)),[2,3]).string(3,'android.intent.category.OPENABLE').invoke('virtual',method(INT,'addCategory',INT,(STR,)),[2,3]).get(3,5,tm).invoke('virtual',method(INT,'setType',INT,(STR,)),[2,3]).string(3,'android.intent.extra.TITLE').invoke('virtual',method(INT,'putExtra',INT,(STR,STR)),[2,3,1]).const(3,1001).invoke('virtual',method(ACT,'startActivityForResult',args=(INT,'I')),[0,2,3]).label('end').ret().label('failure').emit(0x0d|(1<<8)).const(2,0).put(2,0,fp).string(1,'Не удалось открыть окно сохранения.').const(2,1).invoke('static',method(TOAST,'makeText',TOAST,(CTX,'Ljava/lang/CharSequence;','I')),[0,1,2]).result(1).invoke('virtual',method(TOAST,'show'),[1]).ret();a.tries=[('start','end','failure')]
# Result handling for both import and export, explicit errors/cancel.
a=D.code(mc,'onActivityResult',args=('I','I',INT),regs=12,flags=4);a.invoke('super',method(ACT,'onActivityResult',args=('I','I',INT)),[8,9,10,11]).const(0,1002).branch('ne',9,0,'save').get(0,8,fc).branch('eqz',0,None,'return').invoke('static',method(FC,'parseResult','[Landroid/net/Uri;',('I',INT)),[10,11]).result(1).invoke('interface',method(VC,'onReceiveValue',args=(OBJ,)),[0,1]).const(0,0).put(0,8,fc).ret()
a.label('save').const(0,1001).branch('ne',9,0,'return').const(0,-1).branch('ne',10,0,'cancel').branch('eqz',11,None,'cancel').get(0,8,fp).branch('eqz',0,None,'return').const(5,0).label('start')
a.invoke('virtual',method(INT,'getData',URI),[11]).result(1).branch('eqz',1,None,'cancel').invoke('virtual',method(CTX,'getContentResolver','Landroid/content/ContentResolver;'),[8]).result(2).string(3,'wt').invoke('virtual',method('Landroid/content/ContentResolver;','openOutputStream','Ljava/io/OutputStream;',(URI,STR)),[2,1,3]).result(5).string(3,'UTF-8').invoke('virtual',method(STR,'getBytes','[B',(STR,)),[0,3]).result(3).invoke('virtual',method('Ljava/io/OutputStream;','write',args=('[B',)),[5,3]).invoke('virtual',method('Ljava/io/OutputStream;','close'),[5]).label('end').string(1,'Файл сохранён.').go('toast')
a.label('failure').emit(0x0d|(1<<8)).string(1,'Ошибка сохранения. Повторите экспорт в другой файл.').go('toast').label('cancel').string(1,'Сохранение отменено.').label('toast').const(0,0).put(0,8,fp).const(2,1).invoke('static',method(TOAST,'makeText',TOAST,(CTX,'Ljava/lang/CharSequence;','I')),[8,1,2]).result(1).invoke('virtual',method(TOAST,'show'),[1]).label('return').ret();a.tries=[('start','end','failure')]
# Camera permission handling, mirrored by the Java source. No microphone grant.
PR='Landroid/webkit/PermissionRequest;';SA='[Ljava/lang/String;';PT='Lru/viktortown/yarus/CameraPermissionTask;'
fpr=D.field(M,'cameraRequest',PR);mc['fields'].append(fpr)
pc=D.clazz(PT,interfaces=(RUN,));pa=D.field(PT,'activity',M);pp=D.field(PT,'request',PR);pc['fields']=[pa,pp]
a=D.code(pc,'<init>',args=(M,PR),regs=3,flags=0x10001);a.invoke('direct',method(OBJ,'<init>'),[0]).put(1,0,pa).put(2,0,pp).ret()
a=D.code(pc,'run',regs=3);a.get(0,2,pa).get(1,2,pp).invoke('virtual',method(M,'handleCameraRequest',args=(PR,)),[0,1]).ret()
a=D.code(cc,'onPermissionRequest',args=(PR,),regs=4);a.get(0,2,ca).new(1,PT).invoke('direct',method(PT,'<init>',args=(M,PR)),[1,0,3]).invoke('virtual',method(ACT,'runOnUiThread',args=(RUN,)),[0,1]).ret()
a=D.code(cc,'onPermissionRequestCanceled',args=(PR,),regs=4);a.get(0,2,ca).get(1,0,fpr).branch('ne',1,3,'return').const(1,0).put(1,0,fpr).label('return').ret()

def one_string(a,array,size,value,text):
 a.const(size,1).array(array,size,SA).string(value,text).const(size,0).emit(0x4d|(value<<8),array|(size<<8))

# this=v6, request=v7, scratch=v0..5. Deny unknown origins and extra resources.
a=D.code(mc,'handleCameraRequest',args=(PR,),regs=8)
a.invoke('virtual',method(PR,'getOrigin',URI),[7]).result(0)
a.invoke('virtual',method(URI,'getScheme',STR),[0]).result(1).string(2,'https').invoke('virtual',method(STR,'equals','Z',(OBJ,)),[2,1]).result(1,False).branch('eqz',1,None,'deny')
a.invoke('virtual',method(URI,'getHost',STR),[0]).result(1).string(2,'appassets.androidplatform.net').invoke('virtual',method(STR,'equals','Z',(OBJ,)),[2,1]).result(1,False).branch('eqz',1,None,'deny')
a.invoke('virtual',method(URI,'getPort','I'),[0]).result(1,False).const(2,-1).branch('eq',1,2,'origin-port-ok').const(2,443).branch('ne',1,2,'deny').label('origin-port-ok')
a.invoke('virtual',method(PR,'getResources',SA),[7]).result(0).emit(0x21|(1<<8)|(0<<12)).const(2,1).branch('ne',1,2,'deny')
a.const(2,0).emit(0x46|(1<<8),0|(2<<8)).string(2,'android.webkit.resource.VIDEO_CAPTURE').invoke('virtual',method(STR,'equals','Z',(OBJ,)),[2,1]).result(1,False).branch('eqz',1,None,'deny')
a.get(0,6,fpr).branch('eqz',0,None,'store').invoke('virtual',method(PR,'deny'),[0]).label('store').put(7,6,fpr)
a.string(0,'android.permission.CAMERA').invoke('virtual',method(CTX,'checkSelfPermission','I',(STR,)),[6,0]).result(1,False).branch('nez',1,None,'ask')
one_string(a,0,1,2,'android.webkit.resource.VIDEO_CAPTURE');a.invoke('virtual',method(PR,'grant',args=(SA,)),[7,0]).const(0,0).put(0,6,fpr).ret()
a.label('ask');one_string(a,0,1,2,'android.permission.CAMERA');a.const(1,1003).invoke('virtual',method(ACT,'requestPermissions',args=(SA,'I')),[6,0,1]).ret()
a.label('deny').invoke('virtual',method(PR,'deny'),[7]).ret()
# this=v4, requestCode=v5, permissions=v6, grantResults=v7
args=('I',SA,'[I');a=D.code(mc,'onRequestPermissionsResult',args=args,regs=8)
a.invoke('super',method(ACT,'onRequestPermissionsResult',args=args),[4,5,6,7]).const(0,1003).branch('ne',5,0,'return').get(0,4,fpr).branch('eqz',0,None,'return')
a.string(1,'android.permission.CAMERA').invoke('virtual',method(CTX,'checkSelfPermission','I',(STR,)),[4,1]).result(1,False).branch('nez',1,None,'deny')
one_string(a,1,2,3,'android.webkit.resource.VIDEO_CAPTURE');a.invoke('virtual',method(PR,'grant',args=(SA,)),[0,1]).go('clear')
a.label('deny').invoke('virtual',method(PR,'deny'),[0]).label('clear').const(0,0).put(0,4,fpr).label('return').ret()
a=D.code(mc,'onPause',regs=4,flags=4);a.get(0,3,fw).branch('eqz',0,None,'super').string(1,"if(typeof pauseScanner==='function')pauseScanner();").const(2,0).invoke('virtual',method(W,'evaluateJavascript',args=(STR,VC)),[0,1,2]).label('super').invoke('super',method(ACT,'onPause'),[3]).ret()

a=D.code(mc,'onDestroy',regs=3,flags=4);a.get(0,2,fpr).branch('eqz',0,None,'web').invoke('virtual',method(PR,'deny'),[0]).const(0,0).put(0,2,fpr).label('web').get(0,2,fw).branch('eqz',0,None,'super').invoke('virtual',method(W,'destroy'),[0]).label('super').invoke('super',method(ACT,'onDestroy'),[2]).ret()
dex=D.build()
# String pools used by both compiled XML and resources.
def stringpool(strings):
 data=bytearray();offsets=[]
 def l8(n):return bytes([n]) if n<128 else bytes([0x80|(n>>8),n&255])
 for s in strings:
  offsets.append(len(data));v=s.encode();n=len(s.encode('utf-16-le'))//2;data.extend(l8(n)+l8(len(v))+v+b'\0')
 while len(data)%4:data.append(0)
 start=28+4*len(strings);return u16(1)+u16(28)+u32(start+len(data))+u32(len(strings))+u32(0)+u32(0x100)+u32(start)+u32(0)+b''.join(map(u32,offsets))+data
ATTR={'label':0x01010001,'icon':0x01010002,'name':0x01010003,'exported':0x01010010,'configChanges':0x0101001f,'minSdkVersion':0x0101020c,'versionCode':0x0101021b,'versionName':0x0101021c,'windowSoftInputMode':0x0101022b,'targetSdkVersion':0x01010270,'allowBackup':0x01010280,'hardwareAccelerated':0x010102d3,'supportsRtl':0x010103af,'usesCleartextTraffic':0x010104ec,'required':0x0101028e}
ns='http://schemas.android.com/apk/res/android';xs=list(ATTR)+['android',ns,'manifest','package','ru.viktortown.yarus','0.10.0 beta','uses-sdk','uses-permission','android.permission.INTERNET','android.permission.CAMERA','uses-feature','android.hardware.camera','application','ЯРУС','activity','ru.viktortown.yarus.MainActivity','intent-filter','action','android.intent.action.MAIN','category','android.intent.category.LAUNCHER'];xi={s:i for i,s in enumerate(xs)}
chunks=[stringpool(xs),u16(0x180)+u16(8)+u32(8+len(ATTR)*4)+b''.join(map(u32,ATTR.values()))]
def node(t,data):return u16(t)+u16(16)+u32(16+len(data))+u32(1)+u32(0xffffffff)+data
chunks.append(node(0x100,u32(xi['android'])+u32(xi[ns])))
def attr(name,val,typ=3,android=True):
 raw=xi[val] if typ==3 else 0xffffffff;v=xi[val] if typ==3 else val
 return u32(xi[ns] if android else 0xffffffff)+u32(xi[name])+u32(raw)+u16(8)+bytes([0,typ])+u32(v)
def start(name,attrs):chunks.append(node(0x102,u32(0xffffffff)+u32(xi[name])+u16(20)+u16(20)+u16(len(attrs))+u16(0)+u16(0)+u16(0)+b''.join(attrs)))
def end(name):chunks.append(node(0x103,u32(0xffffffff)+u32(xi[name])))
start('manifest',[attr('package','ru.viktortown.yarus',android=False),attr('versionCode',1000,16),attr('versionName','0.10.0 beta')]);start('uses-sdk',[attr('minSdkVersion',26,16),attr('targetSdkVersion',32,16)]);end('uses-sdk');start('uses-permission',[attr('name','android.permission.INTERNET')]);end('uses-permission');start('uses-permission',[attr('name','android.permission.CAMERA')]);end('uses-permission');start('uses-feature',[attr('name','android.hardware.camera'),attr('required',0,18)]);end('uses-feature')
start('application',[attr('label','ЯРУС'),attr('icon',0x7f010000,1),attr('allowBackup',0,18),attr('hardwareAccelerated',0xffffffff,18),attr('supportsRtl',0xffffffff,18),attr('usesCleartextTraffic',0xffffffff,18)])
start('activity',[attr('name','ru.viktortown.yarus.MainActivity'),attr('exported',0xffffffff,18),attr('configChanges',0x6a0,16),attr('windowSoftInputMode',0x10,16)])
start('intent-filter',[]);start('action',[attr('name','android.intent.action.MAIN')]);end('action');start('category',[attr('name','android.intent.category.LAUNCHER')]);end('category');end('intent-filter');end('activity');end('application');end('manifest');chunks.append(node(0x101,u32(xi['android'])+u32(xi[ns])));body=b''.join(chunks);manifest=u16(3)+u16(8)+u32(8+len(body))+body
# Single real launcher icon resource, not a manifest path hack.
globalpool=stringpool(['res/drawable/ic_launcher.png']);typepool=stringpool(['drawable']);keypool=stringpool(['ic_launcher']);typespec=u16(0x202)+u16(16)+u32(20)+bytes([1,0])+u16(0)+u32(1)+u32(0)
config=u32(64)+b'\0'*60;typehead=u16(0x201)+u16(84)+u32(104)+bytes([1,0])+u16(0)+u32(1)+u32(88)+config;typechunk=typehead+u32(0)+u16(8)+u16(0)+u32(0)+u16(8)+bytes([0,3])+u32(0);assert len(typechunk)==104
packagebody=typepool+keypool+typespec+typechunk;name='ru.viktortown.yarus'.encode('utf-16-le').ljust(256,b'\0');packagehead=u16(0x200)+u16(288)+u32(288+len(packagebody))+u32(0x7f)+name+u32(288)+u32(1)+u32(288+len(typepool))+u32(1)+u32(0);assert len(packagehead)==288
restablebody=globalpool+packagehead+packagebody;resources=u16(2)+u16(12)+u32(12+len(restablebody))+u32(1)+restablebody
# Inline app with SHA256 script allowlist. No external script source is allowed.
import runpy
runpy.run_path(str(ROOT/'tools/pack_web.py'),run_name='__main__')
html=(ROOT/'dist/YARUS.html').read_text(encoding='utf-8');icon=(ROOT/'web/logo.svg').read_bytes()

import cairosvg
png=cairosvg.svg2png(bytestring=icon,output_width=256,output_height=256)
# ZIP alignment: resources.arsc stored on a 4-byte boundary as Android requires.
buf=io.BytesIO()
with zipfile.ZipFile(buf,'w') as z:
 for name,data,ctype in [('AndroidManifest.xml',manifest,zipfile.ZIP_DEFLATED),('classes.dex',dex,zipfile.ZIP_DEFLATED),('resources.arsc',resources,zipfile.ZIP_STORED),('res/drawable/ic_launcher.png',png,zipfile.ZIP_STORED),('assets/index.html',html.encode(),zipfile.ZIP_DEFLATED)]:
  zi=zipfile.ZipInfo(name,date_time=(2026,9,16,12,0,0));zi.compress_type=ctype
  if ctype==zipfile.ZIP_STORED:
   offset=z.fp.tell()+30+len(name.encode());pad=(-offset)%4
   if pad:zi.extra=u16(0xFFFF)+u16(pad)+b'\0'*pad
  z.writestr(zi,data)
unsigned=buf.getvalue();eocdpos=unsigned.rfind(b'PK\x05\x06');cdoff=st.unpack_from('<I',unsigned,eocdpos+16)[0]
def digest_sections(sections):
 digests=[]
 for sec in sections:
  for i in range(0,len(sec),1024*1024):chunk=sec[i:i+1024*1024];digests.append(hashlib.sha256(b'\xa5'+u32(len(chunk))+chunk).digest())
 return hashlib.sha256(b'\x5a'+u32(len(digests))+b''.join(digests)).digest()
digest=digest_sections([unsigned[:cdoff],unsigned[cdoff:eocdpos],unsigned[eocdpos:]])
keypath=Path(os.environ.get('YARUS_SIGNING_DIR',str(ROOT.parent/'yarus-private-signing')));keypath.mkdir(exist_ok=True)
if (keypath/'release-key.pem').exists():
 key=serialization.load_pem_private_key((keypath/'release-key.pem').read_bytes(),password=None);cert=x509.load_pem_x509_certificate((keypath/'release-cert.pem').read_bytes())
else:
 if os.environ.get('YARUS_ALLOW_NEW_SIGNING_KEY') != '1':
  raise SystemExit('Existing release-key.pem and release-cert.pem required in YARUS_SIGNING_DIR. Refusing to replace the signing identity.')
 key=rsa.generate_private_key(public_exponent=65537,key_size=2048);subject=x509.Name([x509.NameAttribute(NameOID.COMMON_NAME,'YARUS private beta key')]);now=datetime.datetime.now(datetime.timezone.utc)
 cert=x509.CertificateBuilder().subject_name(subject).issuer_name(subject).public_key(key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-datetime.timedelta(days=1)).not_valid_after(now+datetime.timedelta(days=365*25)).sign(key,hashes.SHA256())
 (keypath/'release-key.pem').write_bytes(key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption()));(keypath/'release-cert.pem').write_bytes(cert.public_bytes(serialization.Encoding.PEM))
der=cert.public_bytes(serialization.Encoding.DER);pub=key.public_key().public_bytes(serialization.Encoding.DER,serialization.PublicFormat.SubjectPublicKeyInfo)
signeddata=LP(LP(u32(0x0103)+LP(digest)))+LP(LP(der))+LP(b'');sig=key.sign(signeddata,padding.PKCS1v15(),hashes.SHA256());signer=LP(signeddata)+LP(LP(u32(0x0103)+LP(sig)))+LP(pub);value=LP(LP(signer));pair=u64(4+len(value))+u32(0x7109871a)+value;size=len(pair)+24;block=u64(size)+pair+u64(size)+b'APK Sig Block 42';eocd=bytearray(unsigned[eocdpos:]);st.pack_into('<I',eocd,16,cdoff+len(block));apk=unsigned[:cdoff]+block+unsigned[cdoff:eocdpos]+eocd
out=OUT/'YARUS-0.10.0-android.apk';out.write_bytes(apk)
# Persist only public build products/source for structural tests.
(OUT/'classes.dex').write_bytes(dex);(ROOT/'docs'/'android-build.json').write_text(json.dumps({'package':'ru.viktortown.yarus','minSdk':26,'targetSdk':32,'version':'0.10.0 beta','dexClasses':len(D.classes),'dexMethods':len(D.mm),'signature':'APK Signature Scheme v2 / RSA PKCS1 SHA256','certificateSHA256':hashlib.sha256(der).hexdigest(),'runtimeTested':False},indent=2))
print(f'APK BUILT: {out}, {len(apk)} bytes, {len(D.classes)} classes, {len(D.mm)} methods; NOT DEVICE-TESTED')
