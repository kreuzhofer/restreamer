"""THROWAWAY #52: deterministic flat pixel-art Blender motion studies.
Run render.py; no production assets, settings or on-air media are changed.
"""
import argparse, json, math, sys
from pathlib import Path
import bpy

parser = argparse.ArgumentParser()
parser.add_argument('--variant', choices=['midnight', 'debug'], required=True)
parser.add_argument('--frames', type=int, nargs='+')
parser.add_argument('--save', action='store_true')
opts = parser.parse_args(sys.argv[sys.argv.index('--') + 1:] if '--' in sys.argv else [])
ROOT = Path(__file__).resolve().parent
OUT = ROOT / 'renders' / opts.variant
OUT.mkdir(parents=True, exist_ok=True)
FONT = json.loads((ROOT.parent / 'font.json').read_text())
ATLAS = json.loads((ROOT.parents[1] / 'maker-workshop/assets/sprite-bounds.json').read_text())
bpy.ops.object.select_all(action='SELECT')
bpy.ops.object.delete(use_global=False)
s = bpy.context.scene
s.render.engine = 'BLENDER_EEVEE'
s.render.resolution_x = 1024
s.render.resolution_y = 576
s.render.resolution_percentage = 100
s.render.fps = 24
s.frame_start = 0
s.frame_end = 383
s.eevee.taa_render_samples = 1
s.render.filter_size = .01
s.render.dither_intensity = 0
s.render.use_motion_blur = False
s.render.image_settings.file_format = 'PNG'
s.render.image_settings.color_mode = 'RGB'
s.view_settings.view_transform = 'Standard'
s.render.threads_mode = 'FIXED'
s.render.threads = 4
COLORS={'ink':'101827','shadow':'080e18','deep':'17263a','steel':'385c70','mid':'547d89',
 'light':'8daeb0','cream':'eee1b9','white':'fff2d0','orange':'e8873c','amber':'ffbf62',
 'rust':'934d36','coral':'d5674c','cyan':'86e3ca','teal':'438c8c','glint':'c4d3c0',
 'steel_dark':'253e54','steel_light':'688e9a','copper':'ba6742','glass':'113438'}
materials=[];lookup={}
for name,value in COLORS.items():
 rgb=[int(value[i:i+2],16)/255 for i in (0,2,4)]
 linear=[v/12.92 if v<=.04045 else ((v+.055)/1.055)**2.4 for v in rgb]
 m=bpy.data.materials.new(name);m.use_nodes=True
 nodes=m.node_tree.nodes;nodes.clear();out=nodes.new('ShaderNodeOutputMaterial');e=nodes.new('ShaderNodeEmission')
 e.inputs['Color'].default_value=(*linear,1);m.node_tree.links.new(e.outputs[0],out.inputs['Surface'])
 lookup[name]=len(materials);materials.append(m)

def rect(sh,x,y,w,h,c):
 x,y,w,h=[round(v*2)/2 for v in (x,y,w,h)]
 if w>0 and h>0:sh.append((x,y,w,h,c))
def box(sh,x,y,w,h,face='steel',rim='light'):
 rect(sh,x,y,w,h,'ink');rect(sh,x+.5,y+.5,w-1,h-1,face)
 rect(sh,x+1,y+.5,w-2,.5,rim);rect(sh,x+.5,y+1,.5,h-2,rim)
def line(sh,x0,y0,x1,y1,c,width=1):
 x0,y0,x1,y1=[round(v*2) for v in (x0,y0,x1,y1)];dx=abs(x1-x0);dy=-abs(y1-y0)
 sx=1 if x0<x1 else -1;sy=1 if y0<y1 else -1;err=dx+dy
 while True:
  rect(sh,(x0+.5)/2-width/2,(y0+.5)/2-width/2,width,width,c)
  if x0==x1 and y0==y1:break
  e=2*err
  if e>=dy:err+=dy;x0+=sx
  if e<=dx:err+=dx;y0+=sy

def disc(sh,x,y,r,c):
 for row in range(-round(r*2),round(r*2)+1):
  yy=row/2;span=math.sqrt(max(0,r*r-yy*yy));rect(sh,x-span,y+yy,span*2,.5,c)
def text(sh,words,x,y,c='cream',scale=1):
 for i,ch in enumerate(words):
  for row,bits in enumerate(FONT.get(ch,[])):
   for col,bit in enumerate(bits):
    if bit=='1':rect(sh,x+(i*6+col)*scale,y+row*scale,scale,scale,c)
def gear(sh,x,y,r,angle,color='steel_light'):
 for i in range(12):
  a=angle+i*math.tau/12;xx=x+math.cos(a)*(r-1);yy=y+math.sin(a)*(r-1)
  rect(sh,xx-1.5,yy-1.5,3,3,'ink');rect(sh,xx-1,yy-1,2,2,color)
 disc(sh,x,y,r-1,'ink');disc(sh,x,y,r-2,color);disc(sh,x,y,r-3,'steel_dark')
 for i in range(6):
  a=angle+i*math.tau/6;line(sh,x+math.cos(a)*2,y+math.sin(a)*2,x+math.cos(a)*(r-4),y+math.sin(a)*(r-4),color,2)
 disc(sh,x,y,2,'ink');disc(sh,x,y,1,'glint')
def geometry(o,sh):
 verts=[];faces=[]
 for i,(x,y,w,h,c) in enumerate(sh):
  n=len(verts);z=i*.00001
  verts.extend([(x,-y,z),(x+w,-y,z),(x+w,-y-h,z),(x,-y-h,z)]);faces.append((n,n+1,n+2,n+3))
 mesh=o.data;mesh.clear_geometry();mesh.from_pydata(verts,[],faces)
 for poly,shape in zip(mesh.polygons,sh):poly.material_index=lookup[shape[4]]
 mesh.update()
def sprite(name,sh,x=0,y=0,z=0):
 mesh=bpy.data.meshes.new(name);o=bpy.data.objects.new(name,mesh);bpy.context.collection.objects.link(o)
 for m in materials:mesh.materials.append(m)
 geometry(o,sh);o.location=(x,288-y,z);return o

def place(o,x,y):o.location.x=round(x*2)/2;o.location.y=288-round(y*2)/2

# Each illustrated component is an independently animated flat textured quad.
# Alpha is clipped at the material boundary, avoiding filtered halos around sprites.
def picture_material(name,path,transparent):
 image=bpy.data.images.load(str(path),check_existing=True);image.pack()
 material=bpy.data.materials.new(name);material.use_nodes=True
 nodes=material.node_tree.nodes;nodes.clear();links=material.node_tree.links
 out=nodes.new('ShaderNodeOutputMaterial');tex=nodes.new('ShaderNodeTexImage');tex.image=image;tex.interpolation='Closest';tex.extension='CLIP'
 emission=nodes.new('ShaderNodeEmission');links.new(tex.outputs['Color'],emission.inputs['Color'])
 if transparent:
  cut=nodes.new('ShaderNodeMath');cut.operation='GREATER_THAN';cut.inputs[1].default_value=.5;links.new(tex.outputs['Alpha'],cut.inputs[0])
  clear=nodes.new('ShaderNodeBsdfTransparent');mix=nodes.new('ShaderNodeMixShader')
  links.new(cut.outputs[0],mix.inputs[0]);links.new(clear.outputs[0],mix.inputs[1]);links.new(emission.outputs[0],mix.inputs[2]);links.new(mix.outputs[0],out.inputs['Surface'])
 else:links.new(emission.outputs[0],out.inputs['Surface'])
 return material,image.size[:]

plate_material, plate_size = picture_material('Illustrated set', ROOT/'assets'/f'{opts.variant}.png', False)
atlas_material, atlas_size = picture_material('Approved workshop robot parts', ROOT.parents[1]/'maker-workshop/assets/robot-parts.png', True)

def picture(name, part=None, z=0):
 mesh=bpy.data.meshes.new(name);o=bpy.data.objects.new(name,mesh);bpy.context.collection.objects.link(o)
 mesh.materials.append(atlas_material if part else plate_material)
 o['crop']=ATLAS['bounds'][part] if part else [0,0,*plate_size]
 o['image_size']=atlas_size if part else plate_size;o.location.z=z
 return o

def pose(o,x,y,w,h,angle=0):
 x0,y0,x1,y1=o['crop'];iw,ih=o['image_size']
 uv=[(x0/iw,1-y0/ih),(x1/iw,1-y0/ih),(x1/iw,1-y1/ih),(x0/iw,1-y1/ih)]
 mesh=o.data;mesh.clear_geometry();mesh.from_pydata([(0,0,0),(w,0,0),(w,-h,0),(0,-h,0)],[],[(0,1,2,3)])
 layer=mesh.uv_layers.get('Sprite crop') or mesh.uv_layers.new(name='Sprite crop')
 for loop in mesh.loops:layer.data[loop.index].uv=uv[loop.vertex_index]
 mesh.update();place(o,x,y);o.rotation_euler[2]=angle

plate=picture('Stationary illustrated set');pose(plate,0,0,512,288)
hero={name:picture('Articulated maker '+name,part,5) for name,part in [('torso','torso'),('left arm','arm'),('right arm','arm'),('left leg','leg'),('right leg','leg'),('head','head')]}
actors=sprite('Moving props',[],z=4)
front=sprite('Faces, tools and effects',[],z=7)
ambient=sprite('CAD and environmental motion',[],z=2)
lettering=sprite('Prototype headline (not editable)',[],z=8)
sh=[]
text(sh,'BE RIGHT BACK',158,32 if opts.variant=='midnight' else 31,'cream',2.5)
text(sh,'MIDNIGHT WORKSHOP' if opts.variant=='midnight' else 'DEBUG QUEST',184 if opts.variant=='midnight' else 210,49 if opts.variant=='midnight' else 57,'amber' if opts.variant=='midnight' else 'cyan',.9)
geometry(lettering,sh)
bpy.ops.object.camera_add(location=(256,144,100));camera=bpy.context.object
camera.name='Flat orthographic camera';camera.rotation_euler=(0,0,0);camera.data.type='ORTHO';camera.data.ortho_scale=512;s.camera=camera

def smooth(v):
 v=max(0,min(1,v));return v*v*(3-2*v)
def mix(a,b,v):return a+(b-a)*smooth(v)
def robot(x,bottom,distance=0,wave=0,hidden=False):
 stride=math.sin(distance/24*math.tau);y=bottom-39-abs(stride)*.6
 for name,o in hero.items():o.hide_render=hidden
 pose(hero['head'],x-5,y-22,35,32)
 pose(hero['torso'],x,y+7,25,22)
 pose(hero['left leg'],x+4+stride*3,y+28-max(0,stride)*2,7,13)
 pose(hero['right leg'],x+15-stride*3,y+28-max(0,-stride)*2,7,13)
 pose(hero['left arm'],x-5,y+10,7,20,angle=-stride*.12)
 pose(hero['right arm'],x+25,y+10,7,20,angle=stride*.12+wave)
 if not hidden:
  for xx in (x+6,x+17):rect(foreground_shapes,xx,y-7,4,3,'cyan');rect(foreground_shapes,xx,y-7,1,1,'white')
  line(foreground_shapes,x+10,y,x+13,y+1,'teal');line(foreground_shapes,x+13,y+1,x+16,y,'teal')

def chip(sh,x,y,good=False):
 for dx in (2,6,10,14):
  rect(sh,x+dx,y-2,1,3,'amber');rect(sh,x+dx,y+12,1,3,'amber')
 box(sh,x,y,18,13,'deep','light');box(sh,x+3,y+3,12,7,'teal' if good else 'rust','cyan' if good else 'coral')
 if good:line(sh,x+6,y+6,x+8,y+8,'cyan');line(sh,x+8,y+8,x+12,y+4,'cyan')
 else:line(sh,x+6,y+4,x+11,y+9,'coral');line(sh,x+11,y+4,x+6,y+9,'coral')

def drone(sh,x,y,t,active):
 # Layered mechanical body, distinct articulated rotors and landing struts.
 for side in (-1,1):
  line(sh,x,y,x+side*18,y-4,'ink',5);line(sh,x,y,x+side*18,y-4,'light',2)
  box(sh,x+side*18-3,y-9,6,7,'orange','amber')
  span=1+abs(math.cos(t*math.tau*6))*10 if active else 11
  line(sh,x+side*18-span,y-10,x+side*18+span,y-10,'steel_dark',3)
  line(sh,x+side*18-span,y-11,x+side*18+span,y-11,'light',1)
  line(sh,x+side*7,y+4,x+side*10,y+13,'ink',3);line(sh,x+side*7,y+4,x+side*10,y+13,'light',1)
  line(sh,x+side*10-4,y+13,x+side*10+4,y+13,'light',2)
 box(sh,x-12,y-5,24,14,'orange','amber');box(sh,x-9,y-3,18,9,'deep','steel')
 for dx in (-5,3):rect(sh,x+dx,y,3,2,'cyan' if active else 'coral')
 disc(sh,x,y-8,3,'ink');disc(sh,x,y-8,2,'cyan' if active else 'coral')
 for dx in (-10,9):rect(sh,x+dx,y-3,1,1,'white')

def bug(sh,x,y,t,good=False):
 for side in (-1,1):
  for i in range(3):
   knee=x+side*(12+math.sin(t*math.tau*4+i)*2)
   line(sh,x+side*7,y-8+i*4,knee,y-7+i*4,'ink',3)
   line(sh,knee,y-7+i*4,knee+side*3,y-2+i*3,'light',1)
 box(sh,x-9,y-13,18,16,'teal' if good else 'rust','cyan' if good else 'coral')
 box(sh,x-6,y-17,12,9,'deep','light')
 for dx in (-4,2):rect(sh,x+dx,y-14,2,2,'cyan' if good else 'amber')
 line(sh,x-4,y-17,x-7,y-22,'light');line(sh,x+4,y-17,x+7,y-22,'light')
 disc(sh,x-7,y-22,1,'cyan' if good else 'coral');disc(sh,x+7,y-22,1,'cyan' if good else 'coral')
 for dx in (-6,-2,2,6):rect(sh,x+dx,y-5,1,4,'amber')

def midnight(t):
 x=mix(170,242,t/3) if t<3 else 242 if t<12 else mix(242,170,(t-12)/4)
 walk=x-170
 wave=(.9+.15*math.sin(t*math.tau*2))*smooth((t-10)/.5)*(1-smooth((t-12)/.5))
 robot(x,190,walk,wave)
 # The same device is checked, flown and parked again: no reset/pop-in.
 flight=smooth((t-7)/1)*(1-smooth((t-10.5)/1.5))
 dx=300+math.sin((t-7)/5*math.tau)*34*flight
 dy=174-43*flight
 drone(actor_shapes,dx,dy,t,7<t<12)
 # Articulated soldering tool approaches, repairs, then retracts clear of flight.
 repair=smooth((t-3)/1)*(1-smooth((t-6)/1))
 tx=mix(362,306,repair);ty=mix(163,171,repair)
 elbowx=350;elbowy=142-12*repair
 for ax,ay,bx,by in ((367,192,elbowx,elbowy),(elbowx,elbowy,tx,ty)):
  line(actor_shapes,ax,ay,bx,by,'ink',7);line(actor_shapes,ax-1,ay-1,bx-1,by-1,'steel_light',4);line(actor_shapes,ax-1,ay-1,bx-1,by-1,'glint',1)
 for xx,yy in ((367,192),(elbowx,elbowy),(tx,ty)):disc(actor_shapes,xx,yy,4,'ink');disc(actor_shapes,xx,yy,2.5,'orange');disc(actor_shapes,xx,yy,1,'amber')
 line(foreground_shapes,tx,ty,tx-5,ty+5,'light',2);line(foreground_shapes,tx-5,ty+5,tx-7,ty+7,'amber')
 if 4<t<6:
  for i in range(6):
   a=i*math.tau/6+t*2;r=3+(t*25+i*3)%9
   rect(foreground_shapes,tx-6+math.cos(a)*r,ty+7+math.sin(a)*r,1,1,'amber' if i%2 else 'white')
 # Rotating CAD is geometry over the empty monitor, not a color wash.
 cube(ambient_shapes,169,111,t)
 for i in range(14):
  xx=19+i*7;yy=12+(t*29+i*19)%88
  line(ambient_shapes,xx,yy,xx-1,yy+3,'mid',.5)
 for i in range(3):
  yy=172-(t*7+i*7)%20;xx=398+math.sin(t*2+i)*2
  rect(ambient_shapes,xx,yy,1,2,'mid')

def cube(sh,x,y,t):
 angle=t*math.tau/16;points=[]
 for zz in (-1,1):
  for yy in (-1,1):
   for xx in (-1,1):
    rx=xx*math.cos(angle)+zz*math.sin(angle);rz=-xx*math.sin(angle)+zz*math.cos(angle)
    points.append((x+rx*9,y+yy*7+rz*3))
 for i in range(8):
  for bit in (1,2,4):
   j=i^bit
   if j>i:line(sh,*points[i],*points[j],'cyan' if i<4 else 'teal',.5)

def debug(t):
 # Characters traverse the whole lane; their return route is hidden behind the
 # foreground apron. Every reset occurs off canvas, never on the visible stage.
 x=mix(-45,209,t/4) if t<4 else 209 if t<8 else mix(209,550,(t-8)/6) if t<14 else 550
 walk=x+45
 robot(x,184,walk,wave=.8*smooth((t-4)/.5)*(1-smooth((t-7.5)/.5)),hidden=x>520)
 bx=mix(-25,253,t/4) if t<4 else 253 if t<8 else mix(253,590,(t-8)/6) if t<14 else 590
 by=180
 if bx<540:bug(actor_shapes,bx,by,t,t>=7)
 if 4<=t<7:
  # Catch, scan, repair: visible scanner beam and screw/repair motion.
  reach=smooth((t-4)/.5)*(1-smooth((t-6.5)/.5))
  line(foreground_shapes,237,158,237+reach*(bx-237),158,'cyan',2)
  line(foreground_shapes,bx-13,149,bx+13,149,'cyan');line(foreground_shapes,bx-13,149,bx-13,175,'cyan');line(foreground_shapes,bx+13,149,bx+13,175,'cyan')
  scan=151+(t*15)%22;line(foreground_shapes,bx-11,scan,bx+11,scan,'white')
  chip(foreground_shapes,246,120,good=False)
  for i in range(3):rect(foreground_shapes,248+i*5,114+(int(t*5)+i)%3,1,2,'amber')
 elif 7<=t<8:
  chip(foreground_shapes,246,120,good=True)
  text(foreground_shapes,'FIXED',243,110,'cyan',.6)
 # Terminal progress cycles completely before the next visitor enters.
 progress=smooth((t-4)/3)*(1-smooth((t-13)/1))
 rect(ambient_shapes,250,151,29*progress,2,'cyan')
 text(ambient_shapes,'RUN' if t<4 or t>=14 else 'SCAN' if t<7 else 'PASS',251,157,'cyan',.55)
 for row in range(3):
  for col in range(6):rect(ambient_shapes,247+col*5,163+row*2,2,1,'cyan' if (row+col+int(t*2))%5<2 else 'steel_dark')
 # Flowing data motes trace a closed circuit, not an overlay rectangle.
 for i in range(12):
  p=(t/16+i/12)%1;x0=30+452*p
  rect(ambient_shapes,x0,224+(i%3)*12,2,1,'cyan' if i%2 else 'amber')

actor_shapes=[];foreground_shapes=[];ambient_shapes=[]
def animate(frame):
 global actor_shapes,foreground_shapes,ambient_shapes
 t=(frame%384)/24
 actor_shapes=[];foreground_shapes=[];ambient_shapes=[]
 midnight(t) if opts.variant=='midnight' else debug(t)
 geometry(actors,actor_shapes);geometry(front,foreground_shapes);geometry(ambient,ambient_shapes)

animate(0)
if opts.save:bpy.ops.wm.save_as_mainfile(filepath=str(ROOT/f'{opts.variant}.blend'))
for frame in opts.frames if opts.frames is not None else range(384):
 animate(frame)
 s.render.filepath=str(OUT/f'{frame:04d}.png')
 bpy.ops.render.render(write_still=True)
