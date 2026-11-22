"""Generate MineWatch's code-drawn chip/pulse icon, with no external dependencies."""
import math,struct,zlib
from pathlib import Path
root=Path(__file__).resolve().parents[1]
def pixel(x,y):
    # A rounded navy tile, a mint microchip, and a rising pulse.
    dx=max(18-x,0,x-110);dy=max(18-y,0,y-110)
    if dx*dx+dy*dy>18*18:return (0,0,0,0)
    c=(14,27,43,255)
    if 32<=x<=96 and 32<=y<=96:c=(45,208,171,255)
    if 39<=x<=89 and 39<=y<=89:c=(17,49,59,255)
    for a in (44,57,70,83):
        if (a-2<=x<=a+2 and (23<=y<=32 or 96<=y<=105)) or (a-2<=y<=a+2 and (23<=x<=32 or 96<=x<=105)):c=(45,208,171,255)
    points=[(45,76),(55,76),(61,57),(69,71),(82,49)]
    for (ax,ay),(bx,by) in zip(points,points[1:]):
        t=max(0,min(1,((x-ax)*(bx-ax)+(y-ay)*(by-ay))/((bx-ax)**2+(by-ay)**2)))
        if math.hypot(x-(ax+t*(bx-ax)),y-(ay+t*(by-ay)))<=2.8:c=(235,255,249,255)
    return c

def raster(n):
    result=[]
    for y in range(n):
        for x in range(n):
            ps=[pixel((x+(a+.5)/3)*128/n,(y+(b+.5)/3)*128/n) for a in range(3) for b in range(3)]
            alpha=sum(p[3] for p in ps)/9
            result.append(tuple(round(sum(p[k]*p[3] for p in ps)/(9*alpha)) if alpha else 0 for k in range(3))+(round(alpha),))
    return result
sizes=[16,24,32,48,64,128,256];entries=[];images=[];offset=6+16*len(sizes)
for n in sizes:
    pixels=raster(n)
    bmp=struct.pack('<IiiHHIIiiII',40,n,n*2,1,32,0,n*n*4,0,0,0,0)
    bmp+=bytes(v for y in reversed(range(n)) for x in range(n) for v in (pixels[y*n+x][2],pixels[y*n+x][1],pixels[y*n+x][0],pixels[y*n+x][3]))
    bmp+=bytes(((n+31)//32)*4*n)
    entries.append(struct.pack('<BBBBHHII',n%256,n%256,0,0,1,32,len(bmp),offset));images.append(bmp);offset+=len(bmp)
(root/'assets/minewatch.ico').write_bytes(struct.pack('<HHH',0,1,len(sizes))+b''.join(entries)+b''.join(images))
n=256;pixels=raster(n)
def chunk(t,d):return struct.pack('>I',len(d))+t+d+struct.pack('>I',zlib.crc32(t+d)&0xffffffff)
raw=b''.join(b'\0'+bytes(v for p in pixels[y*n:(y+1)*n] for v in p) for y in range(n))
(root/'assets/minewatch.png').write_bytes(b'\x89PNG\r\n\x1a\n'+chunk(b'IHDR',struct.pack('>IIBBBBB',n,n,8,6,0,0,0))+chunk(b'IDAT',zlib.compress(raw))+chunk(b'IEND',b''))
