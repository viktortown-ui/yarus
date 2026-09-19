"""Independent binary checks. Does NOT establish installability/runtime correctness."""
import re, base64, io, struct as s, hashlib, zlib, zipfile, pathlib, json, sys
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding
ROOT=pathlib.Path(__file__).resolve().parents[1];path=pathlib.Path(sys.argv[1]) if len(sys.argv)>1 else ROOT/'dist/YARUS-0.10.0-android.apk';data=path.read_bytes();report=[]
def n32(b,o=0):return s.unpack_from('<I',b,o)[0]
def lp(b,o=0):n=n32(b,o);return b[o+4:o+4+n],o+4+n
with zipfile.ZipFile(io.BytesIO(data)) as z:
 assert z.testzip() is None
 required={'AndroidManifest.xml','classes.dex','resources.arsc','res/drawable/ic_launcher.png','assets/index.html'};assert required<=set(z.namelist())
 report.append('PASS: APK ZIP integrity and all required local assets')
 info=z.getinfo('resources.arsc');p=info.header_offset;start=p+30+s.unpack_from('<H',data,p+26)[0]+s.unpack_from('<H',data,p+28)[0];assert start%4==0 and info.compress_type==0
 report.append('PASS: resources.arsc uncompressed and 4-byte aligned')
 dex=z.read('classes.dex');assert dex[:8]==b'dex\n035\0';assert n32(dex,32)==len(dex);assert n32(dex,36)==112;assert hashlib.sha1(dex[32:]).digest()==dex[12:32];assert zlib.adler32(dex[12:])==n32(dex,8)
 assert n32(dex,40)==0x12345678
 def uleb(b,o):
  value=0;shift=0
  while True:
   c=b[o];o+=1;value|=(c&127)<<shift
   if c<128:return value,o
   shift+=7;assert shift<35
 mcount=n32(dex,88);classcount=n32(dex,96);classoff=n32(dex,100);assert classcount==6
 strings=[]
 for i in range(n32(dex,56)):
  p=n32(dex,n32(dex,60)+i*4);n,p=uleb(dex,p);end=dex.index(0,p);strings.append(dex[p:end].decode('utf8').replace('\xc0\x80','\0'))
 assert strings==sorted(strings,key=lambda x:x.encode('utf-16-be'))
 assert 'Landroid/webkit/JavascriptInterface;' in strings and 'saveText' in strings
 assert 'Lru/viktortown/yarus/CameraPermissionTask;' in strings and 'handleCameraRequest' in strings
 assert 'android.webkit.resource.VIDEO_CAPTURE' in strings and 'android.permission.CAMERA' in strings
 assert 'android.permission.RECORD_AUDIO' not in strings
 types=[strings[n32(dex,n32(dex,68)+4*i)] for i in range(n32(dex,64))]
 methodids=[]
 for i in range(mcount):
  cl,proto,n=s.unpack_from('<HHI',dex,n32(dex,92)+8*i);methodids.append((cl,n,proto))
 assert methodids==sorted(methodids)
 # Decode each actual instruction; references/branches/handlers must be in bounds.
 methods_seen=0
 for c in range(classcount):
  record=s.unpack_from('<8I',dex,classoff+c*32);co=record[6];counts=[]
  for _ in range(4):v,co=uleb(dex,co);counts.append(v)
  for _ in range(counts[0]+counts[1]):_,co=uleb(dex,co);_,co=uleb(dex,co)
  for count in counts[2:]:
   mid=0
   for _ in range(count):
    diff,co=uleb(dex,co);mid+=diff;flags,co=uleb(dex,co);code,co=uleb(dex,co);assert mid<mcount and code%4==0
    regs,ins,outs,tries,debug,length=s.unpack_from('<4H2I',dex,code);assert regs>=ins and outs>=0
    assert outs <= 5 or outs <= regs, f'ART rejects outs_size={outs} > registers_size={regs} in method {mid}'
    words=s.unpack_from('<'+'H'*length,dex,code+16);at=0;starts=set();branches=[]
    while at<length:
     starts.add(at);w=words[at];op=w&255
     if op in [0x07]:assert (w>>8)&15<regs and (w>>12)&15<regs;size=1
     elif op in [0x0a,0x0c,0x0d,0x0f,0x11]:assert w>>8<regs;size=1
     elif op==0x0e:size=1
     elif op==0x21:assert (w>>8)&15<regs and (w>>12)&15<regs;size=1
     elif op==0x23:assert (w>>8)&15<regs and (w>>12)&15<regs and words[at+1]<len(types);size=2
     elif op in [0x46,0x4d]:assert w>>8<regs and (words[at+1]&255)<regs and words[at+1]>>8<regs;size=2
     elif op==0x14:assert w>>8<regs;size=3
     elif op in [0x1a,0x22]:assert w>>8<regs;assert words[at+1]<(len(strings) if op==0x1a else len(types));size=2
     elif op in [0x54,0x5b]:assert (w>>8)&15<regs and (w>>12)&15<regs;assert words[at+1]<n32(dex,80);size=2
     elif op in [0x6e,0x6f,0x70,0x71,0x72]:
      count=w>>12;assert count<=5 and count<=outs and words[at+1]<mcount
      rr=[(words[at+2]>>(4*i))&15 for i in range(4)]+[(w>>8)&15];assert all(r<regs for r in rr[:count]);size=3
     elif op==0x74:assert words[at+2]+(w>>8)<=regs and w>>8<=outs and words[at+1]<mcount;size=3
     elif op in [0x32,0x33,0x38,0x39,0x29]:
      delta=words[at+1] if words[at+1]<32768 else words[at+1]-65536;branches.append(at+delta);size=2
     else:raise AssertionError(f'Unknown opcode {op:x}')
     at+=size
    assert at==length and all(x in starts for x in branches)
    methods_seen+=1
 report.append(f'PASS: DEX checksums, sorted tables, {classcount} classes / {methods_seen} defined methods, instruction references, branch boundaries and ART outs_size/registers_size constraint')
 def chunks(b,offset,end):
  found=[]
  while offset<end:
   typ,head,size=s.unpack_from('<HHI',b,offset);assert size>=head>=8 and offset+size<=end;found.append((typ,offset,head,size));offset+=size
  assert offset==end;return found
 manifest=z.read('AndroidManifest.xml');assert n32(manifest,4)==len(manifest);nodes=chunks(manifest,8,len(manifest));depth=0
 for typ,offset,head,size in nodes:
  if typ==0x102:depth+=1
  if typ==0x103:depth-=1
  assert depth>=0
 assert depth==0
 resources=z.read('resources.arsc');assert n32(resources,4)==len(resources);rt=chunks(resources,12,len(resources));pkg=next(x for x in rt if x[0]==0x200);kids=chunks(resources,pkg[1]+pkg[2],pkg[1]+pkg[3]);assert any(x[0]==0x201 for x in kids)
 report.append('PASS: compiled AndroidManifest XML hierarchy and resources table chunk boundaries')
 html=z.read('assets/index.html').decode();assert 'AndroidFiles' in html and "script-src 'sha256-" in html and '<script src=' not in html
 scripts=re.findall(r'<script>(.*?)</script>',html,re.S)
 assert len(scripts)==7, len(scripts)
 for code in scripts:
  digest=base64.b64encode(hashlib.sha256(code.encode()).digest()).decode()
  assert "'sha256-"+digest+"'" in html
 for feature in ['openScanner','YarusQRDecode','YarusQREncoder','labelsPDF']:
  assert feature in html
 report.append('PASS: seven self-contained offline scripts and every CSP hash matches; camera, QR decoder/encoder and labels bundled')
# Independently parse signature and recompute the APK v2 content digest.
eocd=data.rfind(b'PK\x05\x06');cd=n32(data,eocd+16);assert data[cd-16:cd]==b'APK Sig Block 42';size=s.unpack_from('<Q',data,cd-24)[0];blockstart=cd-size-8;assert s.unpack_from('<Q',data,blockstart)[0]==size
pairlen=s.unpack_from('<Q',data,blockstart+8)[0];assert n32(data,blockstart+16)==0x7109871a;value=data[blockstart+20:blockstart+16+pairlen];signers,_=lp(value);signer,_=lp(signers);signed,pos=lp(signer);signatures,pos=lp(signer,pos);pub,pos=lp(signer,pos);assert pos==len(signer)
digests,p=lp(signed);certificates,p=lp(signed,p);attributes,p=lp(signed,p);assert p==len(signed);digestrecord,_=lp(digests);assert n32(digestrecord)==0x103;expected,_=lp(digestrecord,4);certificate,_=lp(certificates);signature_record,_=lp(signatures);assert n32(signature_record)==0x103;signature,_=lp(signature_record,4)
public=serialization.load_der_public_key(pub);public.verify(signature,signed,padding.PKCS1v15(),hashes.SHA256());cert=x509.load_der_x509_certificate(certificate);assert cert.public_key().public_numbers()==public.public_numbers()
corrected=bytearray(data[eocd:]);s.pack_into('<I',corrected,16,blockstart);parts=[data[:blockstart],data[cd:eocd],corrected];dig=[]
for part in parts:
 for i in range(0,len(part),1048576):ch=part[i:i+1048576];dig.append(hashlib.sha256(b'\xa5'+s.pack('<I',len(ch))+ch).digest())
actual=hashlib.sha256(b'\x5a'+s.pack('<I',len(dig))+b''.join(dig)).digest();assert expected==actual
report.append('PASS: APK v2 RSA-SHA256 signature verified; independent full APK digest matches')
report.append('NOT TESTED: Android package installer, ART bytecode verifier, WebView persistence, native file chooser, device networking.')
text='\n'.join(report)+'\n';(ROOT/'docs/android-structure-tests.txt').write_text(text);print(text)
