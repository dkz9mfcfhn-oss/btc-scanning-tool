"""Verify an x64 GUI executable's icon and optional product name/version."""
import struct,sys
from pathlib import Path
b=Path(sys.argv[1]).read_bytes()
source=Path(sys.argv[2]) if len(sys.argv)>2 else Path(__file__).resolve().parents[1]/'assets/minewatch.ico'
ico=source.read_bytes()
u16=lambda p:struct.unpack_from('<H',b,p)[0]
u32=lambda p:struct.unpack_from('<I',b,p)[0]
p=u32(60);assert b[p:p+4]==b'PE\0\0' and u16(p+4)==0x8664
opt=p+24;assert u16(opt+68)==2
sections=[]
for i in range(u16(p+6)):
 o=opt+u16(p+20)+40*i
 sections.append((u32(o+12),u32(o+8),u32(o+20),u32(o+16)))
def offset(rva):
 for va,vs,raw,rs in sections:
  if va<=rva<va+max(vs,rs):return raw+rva-va
 raise ValueError('bad RVA')
base=offset(u32(opt+112+2*8))
def entries(rel):
 o=base+rel
 return dict((u32(o+16+i*8),u32(o+20+i*8)) for i in range(u16(o+12)+u16(o+14)))
def resource(kind,id):
 a=entries(entries(0)[kind]&0x7fffffff)[id]
 leaf=next(iter(entries(a&0x7fffffff).values()))
 o=base+leaf
 pos=offset(u32(o));return b[pos:pos+u32(o+4)]
group=resource(14,1);count=struct.unpack_from('<H',group,4)[0];assert count==7
for i in range(count):
 size,pos=struct.unpack_from('<II',ico,6+16*i+8)
 resourceid=struct.unpack_from('<H',group,6+14*i+12)[0]
 assert resource(3,resourceid)==ico[pos:pos+size]
print('PASS: Windows x64 GUI; icon group ID 1; all 7 icon sizes match source')

if len(sys.argv) > 3:
    expected_name, expected_version = sys.argv[3:5]
    expected_product_version = sys.argv[5] if len(sys.argv) > 5 else expected_version
    info=resource(16,1)

    def version_block(start):
        size, value_length, kind=struct.unpack_from('<HHH', info, start)
        end=start+size
        assert size>=8 and end<=len(info) and kind in (0,1)
        key_end=start+6
        while key_end+2<=end and info[key_end:key_end+2]!=b'\0\0':
            key_end+=2
        assert key_end+2<=end
        key=info[start+6:key_end].decode('utf-16le')
        value_start=(key_end+5)&~3
        value_end=value_start+value_length*(2 if kind else 1)
        assert value_end<=end
        children={}
        child_start=(value_end+3)&~3
        while child_start<end:
            child_key, child_value, descendants, child_end=version_block(child_start)
            assert child_end<=end and child_key not in children
            children[child_key]=(child_value, descendants)
            child_start=(child_end+3)&~3
        return key, info[value_start:value_end], children, end

    key, fixed, children, end=version_block(0)
    assert key=='VS_VERSION_INFO' and end==len(info)
    signature, fmt, file_hi, file_lo, product_hi, product_lo=struct.unpack_from('<6I', fixed)
    assert signature==0xFEEF04BD and fmt==0x10000 and len(fixed)==52
    a,b,c,d=map(int, expected_version.split('.'))
    assert (file_hi,file_lo)==(product_hi,product_lo)==((a<<16)|b,(c<<16)|d)
    translation=children['VarFileInfo'][1]['Translation'][0]
    lang, codepage=struct.unpack('<HH', translation)
    strings=children['StringFileInfo'][1][f'{lang:04x}{codepage:04x}'][1]
    for field,value in {'FileDescription':expected_name, 'ProductName':expected_name,
                        'FileVersion':expected_version, 'ProductVersion':expected_product_version}.items():
        assert strings[field][0].decode('utf-16le')==value+'\0',field
    print(f'PASS: FileDescription and ProductName = {expected_name}; file version = {expected_version}; product version = {expected_product_version}')
