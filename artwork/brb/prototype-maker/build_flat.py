"""THROWAWAY: flat, code-drawn pixel sprites animated and rendered in Blender.
No 3D props, perspective, painted backdrop or image-generation assets.
Blender -b --python build_flat.py -- [--still 139] [--save]
"""
import argparse, json, math, sys
from pathlib import Path
import bpy

parser=argparse.ArgumentParser()
parser.add_argument('--still',type=int)
parser.add_argument('--save',action='store_true')
opts=parser.parse_args(sys.argv[sys.argv.index('--')+1:] if '--' in sys.argv else [])
ROOT=Path(__file__).resolve().parent
OUT=ROOT/'renders'/'flat';OUT.mkdir(parents=True,exist_ok=True)
FONT=json.loads((ROOT.parent/'font.json').read_text())
bpy.ops.object.select_all(action='SELECT');bpy.ops.object.delete(use_global=False)
s=bpy.context.scene;s.render.engine='BLENDER_EEVEE'
s.render.resolution_x=512;s.render.resolution_y=288;s.render.resolution_percentage=100
s.render.fps=24;s.frame_start=0;s.frame_end=287
s.eevee.taa_render_samples=1;s.render.filter_size=.01;s.render.dither_intensity=0
s.render.use_motion_blur=False;s.render.image_settings.file_format='PNG'
s.render.image_settings.color_mode='RGB';s.view_settings.view_transform='Standard'
s.render.threads_mode='FIXED';s.render.threads=4
COLORS={'ink':'101827','deep':'17263a','wall':'1e3448','seam':'28465a','steel':'385c70',
 'mid':'547d89','light':'8daeb0','cream':'eee1b9','white':'fff2d0','orange':'e8873c',
 'amber':'ffbf62','rust':'934d36','coral':'d5674c','red':'f18a63','teal':'438c8c',
 'cyan':'86e3ca','green':'b4cf82','purple':'8e7aab','shadow':'0a121f','blue':'72aec0'}
materials=[];lookup={}
for name,value in COLORS.items():
 rgb=[int(value[i:i+2],16)/255 for i in (0,2,4)]
 linear=[v/12.92 if v<=.04045 else ((v+.055)/1.055)**2.4 for v in rgb]
 m=bpy.data.materials.new(name);m.use_nodes=True
 nodes=m.node_tree.nodes;nodes.clear();out=nodes.new('ShaderNodeOutputMaterial');e=nodes.new('ShaderNodeEmission')
 e.inputs['Color'].default_value=(*linear,1);m.node_tree.links.new(e.outputs[0],out.inputs['Surface'])
 lookup[name]=len(materials);materials.append(m)

def rect(sh,x,y,w,h,c):
 x,y,w,h=map(round,(x,y,w,h))
 if w>0 and h>0:sh.append((x,y,w,h,c))
def box(sh,x,y,w,h,face='steel',rim='light'):
 rect(sh,x,y,w,h,'ink');rect(sh,x+1,y+1,w-2,h-2,face)
 rect(sh,x+2,y+1,w-4,1,rim);rect(sh,x+1,y+2,1,h-4,rim)
def line(sh,x0,y0,x1,y1,c,width=1):
 x0,y0,x1,y1=map(round,(x0,y0,x1,y1));dx=abs(x1-x0);dy=-abs(y1-y0)
 sx=1 if x0<x1 else -1;sy=1 if y0<y1 else -1;err=dx+dy
 while True:
  rect(sh,x0-width//2,y0-width//2,width,width,c)
  if x0==x1 and y0==y1:break
  e=2*err
  if e>=dy:err+=dy;x0+=sx
  if e<=dx:err+=dx;y0+=sy

def text(sh,words,x,y,c='cream',scale=1):
 for i,ch in enumerate(words):
  for row,bits in enumerate(FONT.get(ch,[])):
   for col,bit in enumerate(bits):
    if bit=='1':rect(sh,x+(i*6+col)*scale,y+row*scale,scale,scale,c)

def geometry(o,sh):
 verts=[];faces=[]
 for i,(x,y,w,h,c) in enumerate(sh):
  n=len(verts);z=i*.00001
  verts.extend([(x,-y,z),(x+w,-y,z),(x+w,-y-h,z),(x,-y-h,z)])
  faces.append((n,n+1,n+2,n+3))
 mesh=o.data;mesh.clear_geometry();mesh.from_pydata(verts,[],faces)
 for poly,shape in zip(mesh.polygons,sh):poly.material_index=lookup[shape[4]]
 mesh.update()

def sprite(name,sh,x=0,y=0,z=0):
 mesh=bpy.data.meshes.new(name);o=bpy.data.objects.new(name,mesh);bpy.context.collection.objects.link(o)
 for m in materials:mesh.materials.append(m)
 geometry(o,sh);o.location=(round(x),288-round(y),z);return o

def place(o,x,y):o.location.x=round(x);o.location.y=288-round(y)

# Every visible mark is an integer pixel rectangle in a frontal sprite layer.
a=[];rect(a,0,0,512,288,'ink');rect(a,0,7,512,246,'wall')
for x in range(0,512,32):rect(a,x,8,1,245,'seam')
for y in range(12,252,24):rect(a,0,y,512,1,'seam')
for x in range(16,512,64):
 for y in range(23,246,48):rect(a,x,y,2,2,'steel');rect(a,x+1,y+1,1,1,'ink')
rect(a,0,0,512,7,'shadow');rect(a,0,251,512,37,'deep')
for x in range(0,512,32):line(a,x,254,x-16,288,'seam')
for y in (254,269,284):rect(a,0,y,512,1,'seam')
# Conduits and framed message panel.
for x in (18,487):
 rect(a,x,8,5,238,'ink');rect(a,x+1,8,2,238,'steel')
 for y in (28,91,167,239):box(a,x-2,y,9,5,'steel','mid')
box(a,91,16,330,66,'deep','mid');box(a,99,22,314,52,'ink','steel')
text(a,'BE RIGHT BACK',141,30,'white',3)
text(a,'IDEA  /  BUILD  /  REPEAT',188,61,'amber')
for x in (96,414):
 for y in (20,75):rect(a,x,y,2,2,'cream')
rect(a,108,31,2,29,'amber');rect(a,401,31,2,29,'amber')
# Left terminal, power cabinet and keyboard.
box(a,27,111,75,61,'steel','light');rect(a,33,117,63,44,'shadow')
text(a,'AI / CAD',40,120,'cyan');rect(a,55,172,19,4,'ink');rect(a,46,176,38,4,'steel')
box(a,23,181,84,7,'steel','light')
for x in range(34,94,5):rect(a,x,183,3,2,'cream')
box(a,31,191,69,26,'deep','mid')
for y in (197,202,207):rect(a,39,y,37,2,'steel')
rect(a,87,198,5,5,'amber');rect(a,85,207,9,2,'rust')
# Printer frame sits behind its moving flat sprites.
for x in (121,215):
 box(a,x,100,7,118,'deep','light');rect(a,x+3,108,1,101,'steel')
box(a,119,98,105,9,'steel','light');text(a,'LAYER LAB',139,89,'light')
box(a,125,211,92,8,'deep','mid');rect(a,132,210,78,2,'light')
box(a,200,183,19,19,'steel','mid');rect(a,203,186,10,7,'shadow');rect(a,213,196,3,3,'amber')
# Left spool bracket, wires and upper shelves.
line(a,108,132,116,117,'ink',3);line(a,116,117,164,117,'rust')
line(a,163,116,164,112,'amber');rect(a,38,91,57,4,'steel')
for x,c in ((41,'coral'),(57,'cyan'),(73,'purple')):
 box(a,x,81,11,10,c,'light');rect(a,x+4,83,3,7,'deep')
# Storage, component bins and tools.
box(a,380,171,103,47,'steel','mid')
for x in (384,435):
 for y in (175,189,203):
  box(a,x,y,44,12,'steel','mid');rect(a,x+16,y+4,12,2,'light');rect(a,x+17,y+6,10,1,'ink')
box(a,416,99,67,49,'deep','steel')
for x in range(422,479,8):
 for y in range(104,145,8):rect(a,x,y,1,1,'mid')
for x,c in ((425,'orange'),(436,'cyan'),(447,'orange'),(458,'green'),(470,'coral')):
 rect(a,x,108,2,20,'light');rect(a,x-1,123,4,13,c);rect(a,x,126,1,7,'cream')
box(a,388,155,23,14,'purple','light');box(a,414,154,27,15,'teal','cyan')
rect(a,395,158,9,4,'deep');rect(a,424,157,9,3,'deep')
box(a,449,156,12,12,'cream','white');box(a,460,158,5,7,'cream','white');rect(a,451,155,8,2,'rust')
text(a,'PARTS',447,145,'light')
# Small sketches above the arm, and work lamps.
box(a,248,101,27,31,'cream','white');line(a,253,115,269,109,'steel');line(a,253,115,262,125,'steel');line(a,269,109,262,125,'steel')
rect(a,250,103,5,2,'orange');box(a,282,110,14,17,'amber','cream');rect(a,286,115,6,1,'rust');rect(a,286,119,4,1,'rust')
for x in (45,346):
 rect(a,x+19,8,2,75,'ink');box(a,x,82,41,6,'steel','mid');rect(a,x+5,88,31,2,'cream')
# Conveyor chassis and floor, separate moving tread layer above it.
box(a,21,219,473,27,'steel','light');rect(a,26,222,463,7,'shadow')
for x in range(29,491,19):
 box(a,x,232,9,9,'ink','mid');rect(a,x+3,235,3,3,'light')
for x in (49,148,250,356,467):
 box(a,x,246,7,17,'deep','mid');rect(a,x-4,262,15,3,'shadow')
for x in (68,180,300,416):
 rect(a,x,244,43,5,'amber')
 for d in range(0,40,9):line(a,x+d,244,x+d+4,248,'ink',3)
text(a,'01 GENERATE',35,275,'light');text(a,'02 FABRICATE',180,275,'light');text(a,'03 ASSEMBLE',354,275,'light')
sprite('Flat workshop backdrop',a)

belt=sprite('Moving belt pixels',[],z=1)
monitor=sprite('Animated CAD and AI pixels',[],z=1)
spool=sprite('Turning filament spool',[],x=104,y=135,z=2)
gantry=sprite('Printer gantry',[(0,0,87,3,'mid'),(1,0,85,1,'light')],x=129,y=170,z=2)
a=[];box(a,0,0,16,15,'orange','amber');box(a,3,3,9,9,'deep','cream');rect(a,6,6,3,3,'steel');rect(a,5,15,6,3,'light');rect(a,7,18,2,2,'amber')
nozzle=sprite('Moving extruder sprite',a,z=5)
body_shapes=[]
box(body_shapes,6,3,18,20,'coral','red');box(body_shapes,9,7,12,10,'orange','amber');rect(body_shapes,13,10,4,2,'cyan')
for x in (2,25):
 box(body_shapes,x,7,4,16,'coral','red');box(body_shapes,x,21,4,5,'cream','white')
torso_shapes=body_shapes.copy()
for x in (8,18):
 box(body_shapes,x,23,4,6,'light','cream');rect(body_shapes,x-2,29,8,3,'ink');rect(body_shapes,x-1,29,6,1,'mid')
body=sprite('Printed robot body sprite',body_shapes,z=3)
a=[];box(a,0,0,34,24,'cream','white');rect(a,3,4,28,15,'ink');rect(a,8,9,5,4,'cyan');rect(a,22,9,5,4,'cyan');rect(a,14,16,7,1,'mid');rect(a,15,-4,3,4,'light');rect(a,14,-6,5,3,'amber')
head=sprite('Robot head sprite',a,z=3)
a=[];box(a,0,0,26,8,'deep','mid');box(a,5,-22,16,23,'orange','amber');box(a,8,-29,10,10,'rust','orange');rect(a,9,-17,2,12,'cream')
sprite('Assembly arm pedestal',a,x=319,y=211,z=1)
arm=sprite('Pixel articulated assembly arm',[],z=4)
lights=sprite('Status and print sparks',[],z=6)

bpy.ops.object.camera_add(location=(256,144,100));camera=bpy.context.object
camera.name='Flat orthographic camera';camera.rotation_euler=(0,0,0);camera.data.type='ORTHO';camera.data.ortho_scale=512;s.camera=camera

def smooth(v):v=max(0,min(1,v));return v*v*(3-2*v)
def mix(a,b,v):return a+(b-a)*smooth(v)
def animate(frame):
 t=frame/24;p=min(1,t/3.2)
 x=156 if t<3.2 else mix(156,275,(t-3.2)/2) if t<5.2 else 275 if t<7.3 else mix(275,535,(t-7.3)/4.3)
 walk=0 if t<7.3 else (int((t-7.3)*8)%4)
 body_y=(178 if t<3.2 else mix(178,187,(t-3.2)/2))-(1 if walk in (1,3) else 0)
 body.location.z=5 if t>=6.7 else 3;head.location.z=5 if t>=6.7 else 3
 pose=body_shapes
 if t>=7.3:
  pose=torso_shapes.copy()
  for xx,step in ((8,1),(18,-1)):
   offset=step*(2 if walk==1 else -2 if walk==3 else 0)
   box(pose,xx+offset,23,4,7,'light','cream');rect(pose,xx-2+offset,30,8,2,'ink')
 sh=[];cut=round(32*(1-p))
 for xx,yy,w,h,c in pose:
  top=max(cut,yy);bottom=yy+h
  if top<bottom:rect(sh,xx,top,w,bottom-top,c)
 geometry(body,sh);place(body,x,body_y)
 tip_y=210-round(32*p);nx=165+round(math.sin(t*13)*24) if t<3.2 else 189
 place(nozzle,nx,tip_y-20);place(gantry,129,tip_y-17)
 if t<5.2:hx,hy=349,136
 elif t<6.7:hx,hy=mix(349,273,(t-5.2)/1.5),mix(136,163,(t-5.2)/1.5)
 else:hx,hy=x-2,body_y-24
 place(head,hx,hy)
 if t<6.7:tx,ty=hx+17,hy-2
 else:tx,ty=mix(290,366,(t-6.7)/.8),mix(161,134,(t-6.7)/.8)
 bx,by=332,189;dx,dy=tx-bx,ty-by;d=math.hypot(dx,dy);l1,l2=48,58
 along=(l1*l1-l2*l2+d*d)/(2*d);height=math.sqrt(max(0,l1*l1-along*along))
 ex=bx+along*dx/d+height*dy/d;ey=by+along*dy/d-height*dx/d
 sh=[]
 for ax,ay,cx,cy in ((bx,by,ex,ey),(ex,ey,tx,ty)):
  line(sh,ax,ay,cx,cy,'ink',9);line(sh,ax,ay,cx,cy,'orange',7);line(sh,ax-1,ay-1,cx-1,cy-1,'amber',2)
 for xx,yy in ((bx,by),(ex,ey),(tx,ty)):
  box(sh,xx-4,yy-4,9,9,'steel','light');rect(sh,xx-1,yy-1,3,3,'ink')
 gap=16 if t<6.7 else 24
 for side in (-1,1):
  line(sh,tx+side*3,ty+1,tx+side*gap,ty+4,'light',2);line(sh,tx+side*gap,ty+4,tx+side*gap,ty+11,'light',2)
 geometry(arm,sh)
 sh=[]
 for i in range(60):
  xx=26+(i*8+int(t*20))%460;rect(sh,xx,222,3,6,'mid');rect(sh,xx,222,3,1,'light')
 geometry(belt,sh)
 # Pixel-drawn rotating CAD wireframe and scrolling AI activity.
 sh=[];angle=t*.9;points=[]
 for zz in (-1,1):
  for yy in (-1,1):
   for xx in (-1,1):
    rx=xx*math.cos(angle)+zz*math.sin(angle);rz=-xx*math.sin(angle)+zz*math.cos(angle)
    points.append((65+rx*10,145+yy*8+rz*3))
 for i in range(8):
  for bit in (1,2,4):
   j=i^bit
   if j>i:line(sh,*points[i],*points[j],'cyan' if i<4 else 'teal')
 for i in range(8):rect(sh,38+i*7,156,4,2,'cyan' if (i+int(t*4))%8<4 else 'steel')
 geometry(monitor,sh)
 sh=[];box(sh,-9,-13,18,26,'rust','orange');rect(sh,-10,-12,3,24,'ink');rect(sh,8,-12,3,24,'ink');rect(sh,-4,-10,8,20,'coral')
 for yy in range(-8,9,4):rect(sh,-4,yy,8,1,'red')
 phase=t*5 if t<3.2 else 16
 line(sh,-2,-int(8*math.cos(phase)),3,int(8*math.cos(phase)),'amber',2);rect(sh,-2,-2,5,5,'steel')
 geometry(spool,sh)
 sh=[]
 for xx,start,end in ((27,0,3.2),(172,3.2,5.2),(346,5.2,12)):
  rect(sh,xx,276,4,4,'cyan' if start<=t<end else 'steel')
 for i in range(4):rect(sh,204+i*2,188,1,4,'cyan' if i<int(p*5) else 'steel')
 if t<3.2:
  for i in range(3):rect(sh,nx+3+i*4,tip_y+2+(frame+i)%3,1,1,'amber')
 geometry(lights,sh)

animate(0)
if opts.save:bpy.ops.wm.save_as_mainfile(filepath=str(ROOT/'maker-flat-prototype.blend'))
frames=[opts.still] if opts.still is not None else range(288)
for frame in frames:
 s.frame_set(frame);animate(frame);s.render.filepath=str(OUT/f'{frame:04}.png');bpy.ops.render.render(write_still=True)
