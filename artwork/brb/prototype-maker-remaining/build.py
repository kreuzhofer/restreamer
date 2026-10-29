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
CONCEPT = json.loads((ROOT/'public/concepts.json').read_text())[opts.variant]
DURATION, FPS = CONCEPT['duration'], CONCEPT['fps']
s.render.fps = FPS
s.frame_start = 0
FRAME_COUNT = DURATION * FPS
s.frame_end = FRAME_COUNT - 1
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

STORY = json.loads((ROOT/'assets/friendship-bounds.json').read_text())
story_material, story_size = picture_material('Friendship story props', ROOT/'assets/friendship-parts.png', True)

def picture(name, part=None, z=0, story=False):
 mesh=bpy.data.meshes.new(name);o=bpy.data.objects.new(name,mesh);bpy.context.collection.objects.link(o)
 mesh.materials.append(story_material if story else atlas_material if part else plate_material)
 o['crop']=(STORY if story else ATLAS)['bounds'][part] if part else [0,0,*plate_size]
 o['image_size']=story_size if story else atlas_size if part else plate_size;o.location.z=z
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
visitor={name:picture('Small visitor '+name,part,5) for name,part in [('torso','torso'),('left arm','arm'),('right arm','arm'),('left leg','leg'),('right leg','leg'),('head','head')]}
props={name:picture(name,part,z,True) for name,part,z in [('drone','drone',6),('rotor-left','rotor',7),('rotor-right','rotor',7),('crate','crate',6),('cell','cell',5),('lantern-dark','lantern-dark',8),('lantern-lit','lantern-lit',8)]}
for o in list(visitor.values())+list(props.values()):o.hide_render=opts.variant!='midnight'
doors=sprite('Doorway shutters and threshold',[],z=9)

if opts.variant=='debug':
 frame_mask=picture('Illustrated doorway foreground masks',None,10)
 quads=[]
 for outer_left,outer_right,hole_left in [(17,61,24),(454,503,464)]:
  for row in range(126,183):
   if row<136:quads.append((outer_left,row,outer_right-outer_left,1))
   else:
    inset=max(0,8-(row-136));left=hole_left+inset;right=hole_left+27-inset
    quads.extend([(outer_left,row,left-outer_left,1),(right,row,outer_right-right,1)])
 verts=[];faces=[];uv=[]
 for xx,yy,w,h in quads:
  n=len(verts);verts.extend([(xx,288-yy,0),(xx+w,288-yy,0),(xx+w,288-yy-h,0),(xx,288-yy-h,0)])
  faces.append((n,n+1,n+2,n+3));uv.extend([(xx/512,1-yy/288),((xx+w)/512,1-yy/288),((xx+w)/512,1-(yy+h)/288),(xx/512,1-(yy+h)/288)])
 frame_mask.data.clear_geometry();frame_mask.data.from_pydata(verts,[],faces)
 layer=frame_mask.data.uv_layers.new(name='Original illustrated frame')
 for loop in frame_mask.data.loops:layer.data[loop.index].uv=uv[loop.vertex_index]
 frame_mask.data.update()

actors=sprite('Moving props',[],z=4)
front=sprite('Faces, tools and effects',[],z=10 if opts.variant=='midnight' else 7)
if opts.variant=='midnight':
 for o in visitor.values():o.location.z=9
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
def robot(x,bottom,distance=0,wave=0,hidden=False,size=1,parts=None,left_wave=0,step=1):
 parts=hero if parts is None else parts
 stride=math.sin(distance/24*math.tau)*step;y=bottom-39-abs(stride)*.6
 def part(name,xx,yy,w,h,angle=0):
  pose(parts[name],x+(xx-x)*size,bottom+(yy-bottom)*size,w*size,h*size,angle)
 for o in parts.values():o.hide_render=hidden
 part('head',x-5,y-22,35,32)
 part('torso',x,y+7,25,22)
 part('left leg',x+4+stride*3,y+28-max(0,stride)*2,7,13)
 part('right leg',x+15-stride*3,y+28-max(0,-stride)*2,7,13)
 part('left arm',x-5,y+10,7,20,angle=-stride*.12+left_wave)
 part('right arm',x+25,y+10,7,20,angle=stride*.12+wave)
 faces=[]
 if not hidden:
  for xx in (x+6,x+17):rect(faces,xx,y-7,4,3,'cyan');rect(faces,xx,y-7,1,1,'white')
  line(faces,x+10,y,x+13,y+1,'teal');line(faces,x+13,y+1,x+16,y,'teal')
 foreground_shapes.extend((x+(xx-x)*size,bottom+(yy-bottom)*size,w*size,h*size,c) for xx,yy,w,h,c in faces)

def chip(sh,x,y,good=False):
 for dx in (2,6,10,14):
  rect(sh,x+dx,y-2,1,3,'amber');rect(sh,x+dx,y+12,1,3,'amber')
 box(sh,x,y,18,13,'deep','light');box(sh,x+3,y+3,12,7,'teal' if good else 'rust','cyan' if good else 'coral')
 if good:line(sh,x+6,y+6,x+8,y+8,'cyan');line(sh,x+8,y+8,x+12,y+4,'cyan')
 else:line(sh,x+6,y+4,x+11,y+9,'coral');line(sh,x+11,y+4,x+6,y+9,'coral')


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

def walk_gain(t,segments):
 return max(smooth((t-a)/.3)*(1-smooth((t-b+.3)/.3)) for a,b in segments)

def track(t, keys):
 """Held poses and eased paths make anticipation/reaction beats explicit."""
 for (t0,x0,y0),(t1,x1,y1) in zip(keys,keys[1:]):
  if t<=t1:return mix(x0,x1,(t-t0)/(t1-t0)),mix(y0,y1,(t-t0)/(t1-t0))
 return keys[-1][1:]

def story_drone(x,y,t,heavy=False):
 wobble=math.sin(t*math.tau*1.5)*(.9 if heavy else .25)
 y+=wobble
 pose(props['drone'],x-31,y-20,62,40)
 for side,name in [(-1,'rotor-left'),(1,'rotor-right')]:
  width=12+abs(math.cos(t*math.tau*6))*18
  pose(props[name],x+side*25-width/2,y-24,width,5.5)
 return x,y

def midnight(t):
 # A delivery stop becomes mutual aid. Friends and the replenished shelf return
 # to neutral poses; no repaired character is broken again at the loop boundary.
 x,_=track(t,[(0,170,0),(6,170,0),(10,330,0),(16,330,0),(19,300,0),(22,300,0),(26,244,0),(48,244,0),(52,330,0),(55,330,0),(59,170,0),(60,170,0)])
 helping=smooth((t-9)/1)*(1-smooth((t-15)/1))
 reaching=smooth((t-17)/1)*(1-smooth((t-22)/1))
 holding=smooth((t-34)/1)*(1-smooth((t-47)/1))
 farewell=smooth((t-44)/1)*(1-smooth((t-48)/1))
 launch=smooth((t-52)/1)*(1-smooth((t-55)/1))
 arm=.65*helping+1.9*reaching+.7*holding+(.9+.13*math.sin(t*math.tau))*farewell+.65*launch
 distance=x-170 if t<10 or t>55 else x-330 if t<22 else x-244
 if 10<=t<16 or 19<=t<22 or 26<=t<48 or 52<=t<55:distance=0
 robot(x,190,distance,arm,step=walk_gain(t,[(6,10),(16,19),(22,26),(48,52),(55,59)]))
 # The maker visibly sets the tool down before leaving their own work.
 tool_work=1-smooth((t-5)/1) if t<6 else smooth((t-58)/1)
 toolx=mix(201,198,tool_work)
 tooly=mix(187,179-math.sin(t*math.tau*2),tool_work)
 line(foreground_shapes,toolx,tooly,toolx+7,tooly-5,'light',2)
 box(actor_shapes,196,183,15,6,'deep','light')
 dx,dy=track(t,[(0,-70,124),(2,-70,124),(8,380,135),(12,380,135),(16,380,112),(19,351,84),(23,351,84),(26,293,123),(34,293,123),(42,293,123),(46,305,113),(48,380,140),(49,380,140),(52,351,84),(53,380,135),(55,380,135),(59,580,105),(60,580,105)])
 dx,dy=story_drone(dx,dy,t,2<t<12)
 # Cargo follows the hook while carried, rests on the bench during the visit,
 # and is lifted again with the maker's help before the drone flies onward.
 if t<12:cx,cy=dx,dy+30
 elif t<14:cx,cy=380,mix(165,166,(t-12)/2)
 elif t<54:cx,cy=380,166
 elif t<55:cx,cy=380,mix(166,dy+30,(t-54))
 else:cx,cy=dx,dy+30
 pose(props['crate'],cx-17,cy,34,24)
 if t<12 or t>=54:line(foreground_shapes,dx,dy+18,cx,cy,'light',1)
 # Drone retrieves the high shelf cell after the maker's failed reach.
 # A fresh cell is later lifted from inside the opaque crate to restock the shelf.
 if t<21:cellx,celly=351,110
 elif t<23:cellx,celly=351,mix(110,dy+26,(t-21)/2)
 elif t<36:cellx,celly=dx,dy+26
 elif t<42:cellx,celly=293,mix(149,174,(t-36)/6)
 elif t<48:cellx,celly=293,174
 elif t<49:cellx,celly=380,mix(170,dy+26,(t-48))
 elif t<51.5:cellx,celly=dx,dy+26
 elif t<52:cellx,celly=dx,mix(dy+26,110,(t-51.5)/.5)
 else:cellx,celly=351,110
 props['cell'].hide_render=42<=t<48
 pose(props['cell'],cellx-4,celly,8,18)
 if 21<t<42 or 48<t<52:line(foreground_shapes,dx,dy+18,cellx,celly,'light',.75)
 elif 42<=t<44:line(foreground_shapes,dx,dy+18,dx,dy+18+33*(1-smooth((t-42)/2)),'light',.75)
 rect(ambient_shapes,337,129,29,2,'steel_dark');rect(ambient_shapes,337,128,29,1,'light')
 # A small visitor arrives with an unlit lantern, then carries the lit one away.
 vx,_=track(t,[(0,550,0),(26,550,0),(33,330,0),(50,330,0),(54,550,0),(60,550,0)])
 celebrate=smooth((t-44)/.5)*(1-smooth((t-48)/.5))
 bob=abs(math.sin((t-44)*math.tau))*2*celebrate
 visitor_bottom=201-11*smooth((t-31)/2) if t<33 else 190+11*smooth((t-50)/1)
 robot(vx,visitor_bottom-bob,vx-330,wave=celebrate*(1.2+.2*math.sin(t*math.tau)),size=.62,parts=visitor,left_wave=-.5,step=walk_gain(t,[(26,33),(50,54)]))
 if t<34:lx=vx-12
 elif t<36:lx=mix(318,293,(t-34)/2)
 elif t<48:lx=293
 elif t<50:lx=mix(293,318,(t-48)/2)
 else:lx=vx-12
 for name in ('lantern-dark','lantern-lit'):
  props[name].hide_render=(name=='lantern-lit')!=(t>=42)
  lantern_y=162+(visitor_bottom-190 if t<34 or t>=50 else 0)-bob
  pose(props[name],lx-8,lantern_y,16,28)
 # Shared celebration is a response to the repaired object, not a text caption.
 if 42<t<44:
  for i in range(5):
   angle=i*math.tau/5;radius=16+(t-42)*5
   rect(foreground_shapes,lx+math.cos(angle)*radius,176+math.sin(angle)*radius,1,1,'amber')
 cube(ambient_shapes,169,111,t*16/60)
 for i in range(14):
  xx=19+i*7;yy=12+(t*(88/3)+i*19)%88
  line(ambient_shapes,xx,yy,xx-1,yy+3,'mid',.5)
 for i in range(3):rect(ambient_shapes,398+math.sin(t*math.tau/3+i)*2,172-(t*5+i*7)%20,1,2,'mid')

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

def shutter(sh,x,openness):
 # Fit the illustrated arched aperture. The opaque shutter retracts upward;
 # all actor relocation occurs behind fully closed shutters, never on the lane.
 for row in range(46):
  inset=max(0,8-row) if row<8 else 0
  if row<46*(1-openness):
   rect(sh,x+inset,136+row,27-inset*2,1,'steel_dark' if row%5 else 'light')
   if row%5==1:rect(sh,x+inset+1,136+row,25-inset*2,.5,'steel')
 rect(sh,x+5,181-45*openness,17,1,'amber')

def debug(t):
 # Actors start inside the left doorway, step toward the viewer onto the lane,
 # and enter the right doorway before its shutter conceals the periodic reset.
 entry=smooth((t-1)/1.5)
 if t<2.5:rx,rb,rs=38,179+11*entry,.72+.28*entry
 elif t<5:rx,rb,rs=mix(38,222.5,(t-2.5)/2.5),190,1
 elif t<8.5:rx,rb,rs=222.5,190,1
 elif t<13:rx,rb,rs=mix(222.5,477,(t-8.5)/4.5),190,1
 else:
  entering=smooth((t-13)/1.5);rx,rb,rs=477,190-11*entering,1-.28*entering
 x=rx-13.5*rs
 wave=.8*smooth((t-5)/.5)*(1-smooth((t-8)/.5))
 walking=rx-38 if 2.5<t<5 else rx-222.5 if 8.5<t<13 else 0
 robot(x,rb,walking,wave,size=rs,step=walk_gain(t,[(2.5,5),(8.5,13)]))
 in_front=rb>=184
 for o in hero.values():o.location.z=11 if in_front else 5
 front.location.z=12 if in_front else 7
 if t<2:
  out=smooth((t-.6)/1.4);bx,by,bs=38,176+10*out,.7+.3*out
 elif t<5:bx,by,bs=mix(38,264,(t-2)/3),186,1
 elif t<8.5:bx,by,bs=264,186,1
 elif t<12.5:bx,by,bs=mix(264,477,(t-8.5)/4),186,1
 else:
  entering=smooth((t-12.5)/1.2);bx,by,bs=477,186-10*entering,1-.3*entering
 actors.location.z=11 if by>=181 else 4
 shapes=[];bug(shapes,bx,by,t,t>=7.5)
 actor_shapes.extend((bx+(xx-bx)*bs,by+(yy-by)*bs,w*bs,h*bs,c) for xx,yy,w,h,c in shapes)
 if 5<=t<7.5:
  reach=smooth((t-5)/.5)*(1-smooth((t-7)/.5))
  line(foreground_shapes,237,164,237+reach*(bx-237),164,'cyan',2)
  line(foreground_shapes,bx-13,155,bx+13,155,'cyan');line(foreground_shapes,bx-13,155,bx-13,181,'cyan');line(foreground_shapes,bx+13,155,bx+13,181,'cyan')
  scan=157+(t*15)%22;line(foreground_shapes,bx-11,scan,bx+11,scan,'white')
  chip(foreground_shapes,257,126,good=False)
 elif 7.5<=t<8.5:chip(foreground_shapes,257,126,good=True)
 progress=smooth((t-5)/2.5)*(1-smooth((t-14)/1))
 rect(ambient_shapes,250,151,29*progress,2,'cyan')
 text(ambient_shapes,'RUN' if t<5 or t>=15 else 'SCAN' if t<7.5 else 'PASS',251,157,'cyan',.55)
 for row in range(3):
  for col in range(6):rect(ambient_shapes,247+col*5,163+row*2,2,1,'cyan' if (row+col+int(t*2))%5<2 else 'steel_dark')
 for i in range(12):
  p=(t/16+i/12)%1;rect(ambient_shapes,30+452*p,224+(i%3)*12,2,1,'cyan' if i%2 else 'amber')
 door_shapes=[]
 shutter(door_shapes,24,smooth(t/.6)*(1-smooth((t-2.8)/.6)))
 shutter(door_shapes,464,smooth((t-11)/.7)*(1-smooth((t-14.5)/.8)))
 geometry(doors,door_shapes)

actor_shapes=[];foreground_shapes=[];ambient_shapes=[]
def animate(frame):
 global actor_shapes,foreground_shapes,ambient_shapes
 t=(frame%FRAME_COUNT)/FPS
 actor_shapes=[];foreground_shapes=[];ambient_shapes=[]
 midnight(t) if opts.variant=='midnight' else debug(t)
 geometry(actors,actor_shapes);geometry(front,foreground_shapes);geometry(ambient,ambient_shapes)

animate(0)
if opts.save:bpy.ops.wm.save_as_mainfile(filepath=str(ROOT/f'{opts.variant}.blend'))
for frame in opts.frames if opts.frames is not None else range(FRAME_COUNT):
 animate(frame)
 s.render.filepath=str(OUT/f'{frame:04d}.png')
 bpy.ops.render.render(write_still=True)
