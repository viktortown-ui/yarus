#!/usr/bin/env python3
"""Attach an icon and asInvoker/DPI manifest to an unsigned Go Windows PE.
Usage: python tools/pe_icon.py dist/YARUS.exe branding/YARUS.ico
"""
import struct as s, sys
from pathlib import Path
u16=lambda x:s.pack('<H',x);u32=lambda x:s.pack('<I',x)
path=Path(sys.argv[1]);ico=Path(sys.argv[2]).read_bytes();b=bytearray(path.read_bytes());pe=s.unpack_from('<I',b,60)[0];n=s.unpack_from('<H',b,pe+6)[0];op=pe+24;optsize=s.unpack_from('<H',b,pe+20)[0];table=op+optsize
assert s.unpack_from('<H',b,op)[0]==0x20b
align=lambda n,a:(n+a-1)//a*a
sa,fa=s.unpack_from('<II',b,op+32);header_size=s.unpack_from('<I',b,op+60)[0];assert table+(n+1)*40<=header_size
lastva=max(s.unpack_from('<I',b,table+40*i+12)[0]+s.unpack_from('<I',b,table+40*i+8)[0] for i in range(n));rva=align(lastva,sa);raw=align(len(b),fa)
_,typ,count=s.unpack_from('<HHH',ico);assert typ==1
icons={};group=u16(0)+u16(1)+u16(count)
for i in range(count):
 w,h,colors,res,planes,bits,size,off=s.unpack_from('<BBBBHHII',ico,6+i*16);icons[i+1]={0:ico[off:off+size]};group+=s.pack('<BBBBHHIH',w,h,colors,res,planes,bits,size,i+1)
manifest=b'''<?xml version="1.0" encoding="UTF-8" standalone="yes"?><assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0"><assemblyIdentity version="0.10.0.0" processorArchitecture="amd64" name="YARUS.Warehouse" type="win32"/><description>YARUS Warehouse</description><trustInfo xmlns="urn:schemas-microsoft-com:asm.v3"><security><requestedPrivileges><requestedExecutionLevel level="asInvoker" uiAccess="false"/></requestedPrivileges></security></trustInfo><application xmlns="urn:schemas-microsoft-com:asm.v3"><windowsSettings><dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</dpiAware></windowsSettings></application></assembly>'''
tree={3:icons,14:{1:{0:group}},24:{1:{0:manifest}}};res=bytearray();leaves=[]
def directory(d):
 start=len(res);res.extend(b'\0'*12+u16(0)+u16(len(d))+b'\0'*(len(d)*8))
 for i,(key,v) in enumerate(sorted(d.items())):
  pos=start+16+i*8;s.pack_into('<I',res,pos,key)
  if isinstance(v,dict):child=directory(v);s.pack_into('<I',res,pos+4,child|0x80000000)
  else:leaf=len(res);res.extend(b'\0'*16);leaves.append((leaf,v));s.pack_into('<I',res,pos+4,leaf)
 return start
directory(tree)
for leaf,data in leaves:
 res.extend(b'\0'*((-len(res))%4));pos=len(res);res.extend(data);s.pack_into('<IIII',res,leaf,rva+pos,len(data),0,0)
size=len(res);rawsize=align(size,fa);b.extend(b'\0'*(raw-len(b)));b.extend(res);b.extend(b'\0'*(rawsize-size))
s.pack_into('<8sIIIIIIHHI',b,table+n*40,b'.rsrc\0\0\0',size,rva,rawsize,raw,0,0,0,0,0x40000040)
s.pack_into('<H',b,pe+6,n+1);s.pack_into('<I',b,op+56,align(rva+size,sa));s.pack_into('<II',b,op+112+16,rva,size);s.pack_into('<I',b,op+8,s.unpack_from('<I',b,op+8)[0]+rawsize);s.pack_into('<I',b,op+64,0)
path.write_bytes(b);print(f'Embedded {count} icon sizes and asInvoker manifest into {path.name}')
