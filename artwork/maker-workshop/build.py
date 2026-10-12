"""Author the Maker Workshop's prepared stage masters in Blender.
Art assets: assets/prompts.json. No Blender dependency at server runtime.
Blender -b --python build.py -- --season workshop --stage brb [--frames 0 287 288]
"""
import argparse, json, math, sys
from pathlib import Path
import bpy

parser=argparse.ArgumentParser()
parser.add_argument('--still',type=int)
parser.add_argument('--frames',type=int,nargs='+',help='Render selected inspection frames, including 288 for the loop endpoint')
parser.add_argument('--save',action='store_true')
parser.add_argument('--season',choices=['workshop','halloween','christmas'],default='workshop')
parser.add_argument('--stage',choices=['brb','prestream','ending'],default='brb')
opts=parser.parse_args(sys.argv[sys.argv.index('--')+1:] if '--' in sys.argv else [])
ROOT=Path(__file__).resolve().parent
OUT=ROOT/'renders'/opts.season/opts.stage;OUT.mkdir(parents=True,exist_ok=True)
FONT=json.loads((ROOT.parent/'brb'/'font.json').read_text())
ATLAS=json.loads((ROOT/'assets'/'sprite-bounds.json').read_text())
SEASONAL=json.loads((ROOT/'assets'/'seasonal-bounds.json').read_text())
bpy.ops.object.select_all(action='SELECT');bpy.ops.object.delete(use_global=False)
s=bpy.context.scene;s.render.engine='BLENDER_EEVEE'
s.render.resolution_x=1024;s.render.resolution_y=576;s.render.resolution_percentage=100
s.render.fps=24;s.frame_start=0;s.frame_end=287
s.eevee.taa_render_samples=1;s.render.filter_size=.01;s.render.dither_intensity=0
s.render.use_motion_blur=False;s.render.image_settings.file_format='PNG'
s.render.image_settings.color_mode='RGB';s.view_settings.view_transform='Standard'
s.render.threads_mode='FIXED';s.render.threads=4
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
plate_material,plate_size=picture_material('Illustrated workshop',ROOT/'assets'/f'{opts.season}.png',False)
atlas_material,atlas_size=picture_material('Illustrated sprite parts',ROOT/'assets'/'robot-parts.png',True)
season_material,season_size=picture_material('Seasonal accessories',ROOT/'assets'/'seasonal-parts.png',True)

def picture(name,part,z,seasonal=False):
 mesh=bpy.data.meshes.new(name);o=bpy.data.objects.new(name,mesh);bpy.context.collection.objects.link(o)
 mesh.materials.append(season_material if seasonal else atlas_material if part else plate_material)
 o['crop']=(SEASONAL if seasonal else ATLAS)['bounds'][part] if part else [0,0,*plate_size]
 o['image_size']=season_size if seasonal else atlas_size if part else plate_size;o.location.z=z
 return o

def pose(o,x,y,w,h,trim=0,pivot=0,angle=0):
 o.hide_render=trim>=h
 if o.hide_render:return
 trim=max(0,trim);x0,y0,x1,y1=o['crop'];iw,ih=o['image_size']
 top=1-(y0+(y1-y0)*trim/h)/ih;bottom=1-y1/ih
 uv=[(x0/iw,top),(x1/iw,top),(x1/iw,bottom),(x0/iw,bottom)]
 px,py=w*pivot,h*pivot
 mesh=o.data;mesh.clear_geometry();mesh.from_pydata([(-px,py-trim,0),(w-px,py-trim,0),(w-px,py-h,0),(-px,py-h,0)],[],[(0,1,2,3)])
 layer=mesh.uv_layers.get('Sprite crop') or mesh.uv_layers.new(name='Sprite crop');mesh.uv_layers.active=layer
 for loop in mesh.loops:layer.data[loop.index].uv=uv[loop.vertex_index]
 mesh.update();place(o,x+px,y+py);o.rotation_euler[2]=angle

plate=picture('Detailed stationary workshop plate',None,0);pose(plate,0,0,512,288)
hero={name:picture('Hero '+name,part,3) for name,part in [('torso','torso'),('left arm','arm'),('right arm','arm'),('left leg','leg'),('right leg','leg'),('head','head')]}
next_head=picture('Next head delivery shuttle','head',3)
extruder=picture('Moving printer extruder','extruder',5)
spool=picture('Rotating filament spool','spool',2)
helper={name:picture('Maintenance helper '+name,part,7) for name,part in [('torso','torso'),('left arm','arm'),('right arm','arm'),('left leg','leg'),('right leg','leg'),('head','head')]}
monitor=sprite('Animated AI and CAD screen',[],z=1)
belt=sprite('Moving conveyor tread',[],z=1)
gantry=sprite('Printer gantry',[(0,0,74,3,'steel'),(1,0,72,.5,'glint'),(1,2,72,.5,'ink')],z=2)
arm=sprite('Articulated pixel assembly arm',[],z=4)
mechanics=sprite('Conveyor gears and flexible arm hose',[],z=2)
face=sprite('Robot boot sequence and facial expressions',[],z=6)
helper_face=sprite('Maintenance helper expression',[],z=8)
sparks=sprite('Extrusion and machine indicators',[],z=6)
shuttle=sprite('Head supply trolley',[],z=2)
season_parts={}
if opts.season!='workshop':
 hat='witch-hat' if opts.season=='halloween' else 'santa-hat'
 for name,part in [('hero-hat',hat),('supply-hat',hat),('helper-hat',hat),('prop','ghost' if opts.season=='halloween' else 'gift')]:
  season_parts[name]=picture(name,part,9,True)
 if opts.season=='christmas':season_parts['scarf']=picture('Robot knitted scarf','scarf',9,True)
ambient=sprite('Seasonal moving accents',[],z=8)
a=[];box(a,319,205,26,8,'deep','mid');box(a,324,183,16,23,'orange','amber');box(a,327,176,10,10,'rust','orange')
for xx in (322,341):disc(a,xx,209,1,'glint')
rect(a,325,185,1,17,'cream');rect(a,337,185,2,17,'rust')
sprite('Assembly pedestal',a,z=1)
# A rail gives the incoming head a visible delivery path rather than a loop teleport.
a=[];rect(a,348,119,164,2,'shadow');rect(a,348,119,164,.5,'mid');rect(a,348,121,164,.5,'copper')
sprite('Head supply rail',a,z=1)

bpy.ops.object.camera_add(location=(256,144,100));camera=bpy.context.object
camera.name='Flat orthographic camera';camera.rotation_euler=(0,0,0);camera.data.type='ORTHO';camera.data.ortho_scale=512;s.camera=camera

def smooth(v):v=max(0,min(1,v));return v*v*(3-2*v)
def mix(a,b,v):return a+(b-a)*smooth(v)
def animate(frame):
 # Every moving system uses the same periodic clock, including inspection frame 288.
 ending=opts.stage=='ending'
 frame=min(frame,287) if ending else frame%288
 t=frame/24
 # Starting-soon opens on the assembled mascot, then cycles through production.
 if opts.stage=='prestream':t=(t+7.6)%12
 clock=min(t,6) if ending else t
 p=1 if ending else smooth(t/3.2)
 x=156 if t<3.2 else mix(156,275,(t-3.2)/2) if t<5.2 else 275 if t<7.6 else mix(275,540,(t-7.6)/4)
 if ending:x=mix(275,540,(t-1.2)/4.8)
 walking=ending or t>=7.6;phase=(x-275)/32*math.tau if walking else 0;stride=math.sin(phase)
 body_y=(151 if t<3.2 else mix(151,180,(t-3.2)/2))-(abs(stride)*.75 if walking else 0)
 if ending:body_y=180-abs(stride)*.75
 cut=33*(1-p)
 parts=[('torso',3,0,24,21),('left arm',-3,3,8,20),('right arm',27,3,8,20),('left leg',7,20,7,13),('right leg',18,20,7,13)]
 for name,xx,yy,w,h in parts:
  direction=1 if name.startswith('left') else -1
  angle=0
  if walking and 'leg' in name:xx+=stride*direction*3;yy-=max(0,stride*direction)*2.5
  if walking and 'arm' in name:angle=-stride*direction*.18
  greeting=t+7.05 if ending else t
  if name=='right arm':angle+=smooth((greeting-7.05)/.3)*(1-smooth((greeting-8.15)/.35))*(1.9+.16*math.sin(greeting*math.tau*2))
  o=hero[name];o.location.z=5 if ending or t>=6.7 else 3
  pose(o,x+xx,body_y+yy,w,h,trim=cut-yy,angle=angle)
 if ending:hx,hy=x-2,body_y-24
 elif t<5.2:hx,hy=349,136
 elif t<6.7:hx,hy=mix(349,273,(t-5.2)/1.5),mix(136,156,(t-5.2)/1.5)
 else:hx,hy=x-2,body_y-24
 hero['head'].location.z=5 if ending or t>=6.7 else 3;pose(hero['head'],hx-3,hy-12,40,36)
 if ending:tip_y=mix(151,184,t/6);nx=mix(207,172,t/6)
 elif t<3.2:tip_y=184-33*p;nx=172+math.sin(t/3.2*math.tau*8)*16*smooth(t/.3)*smooth((3.2-t)/.3)
 elif t<10.5:tip_y=151;nx=mix(172,207,(t-3.2)/.5)
 else:tip_y=mix(151,184,(t-10.5)/1.5);nx=mix(207,172,(t-10.5)/1.5)
 pose(extruder,nx-9,tip_y-28,18,28);place(gantry,139,tip_y-22)
 pose(spool,99,126,22,22,pivot=.5,angle=math.tau*2*p)
 if ending:tx,ty=366,134
 elif t<6.7:tx,ty=hx+17,hy-2
 else:tx,ty=mix(290,366,(t-6.95)/.55),mix(154,134,(t-6.95)/.55)
 bx,by=332,184;dx,dy=tx-bx,ty-by;d=math.hypot(dx,dy);l1,l2=48,58
 along=(l1*l1-l2*l2+d*d)/(2*d);height=math.sqrt(max(0,l1*l1-along*along))
 ex=bx+along*dx/d+height*dy/d;ey=by+along*dy/d-height*dx/d
 sh=[]
 for ax,ay,cx,cy in ((bx,by,ex,ey),(ex,ey,tx,ty)):
  line(sh,ax,ay,cx,cy,'ink',11);line(sh,ax,ay,cx,cy,'rust',9)
  line(sh,ax-1,ay-1,cx-1,cy-1,'orange',7);line(sh,ax-2,ay-2,cx-2,cy-2,'amber',2)
  line(sh,ax+1,ay+1,cx+1,cy+1,'copper')
 for xx,yy in ((bx,by),(ex,ey),(tx,ty)):
  disc(sh,xx,yy,5,'ink');disc(sh,xx-.5,yy-.5,4,'steel_light');disc(sh,xx,yy,2.5,'steel_dark');disc(sh,xx,yy,1,'glint')
  for angle in (0,math.pi/2,math.pi,math.pi*1.5):rect(sh,xx+math.cos(angle)*3,yy+math.sin(angle)*3,.5,.5,'ink')
 gap=16 if t<6.7 else mix(16,24,(t-6.7)/.25) if t<11.1 else mix(24,16,(t-11.1)/.3)
 if ending:gap=24
 for side in (-1,1):
  line(sh,tx+side*3,ty+1,tx+side*gap,ty+4,'light',2);line(sh,tx+side*gap,ty+4,tx+side*gap,ty+11,'light',2)
 geometry(arm,sh)
 sh=[]
 for i in range(58):
  belt_time=(clock-clock*clock/12) if ending else t
  xx=26+(i*8+int(belt_time*20))%464
  line(sh,xx,216,xx-3,221,'steel',2);line(sh,xx,216,xx-3,221,'mid')
 geometry(belt,sh)
 # The CAD drawing and UI activity are rendered independently over the empty screen.
 sh=[];text(sh,'REST' if ending and t>=6 else 'READY' if opts.stage=='prestream' else 'AI / CAD',38,113,'cyan',.75);angle=clock*math.tau/12;points=[]
 for zz in (-1,1):
  for yy in (-1,1):
   for xx in (-1,1):
    rx=xx*math.cos(angle)+zz*math.sin(angle);rz=-xx*math.sin(angle)+zz*math.cos(angle)
    zoom=1-smooth((t-3)/3) if ending else 1
    points.append((64+rx*10*zoom,132+(yy*8+rz*3)*zoom))
 for i in range(8):
  for bit in (1,2,4):
   j=i^bit
   if j>i and not (ending and t>=6):line(sh,*points[i],*points[j],'cyan' if i<4 else 'teal',.5)
 for i in range(8):rect(sh,38+i*6,149,3,1,'cyan' if not (ending and t>=6) and (i+int(clock*4))%8<4 else 'steel')
 geometry(monitor,sh)
 sh=[]
 for i,xx in enumerate((45,142,240,345,470)):gear(sh,xx,231,6,belt_time*math.tau/3*(1 if i%2 else -1),'orange' if i==2 else 'steel_light')
 line(sh,bx+5,by+3,ex+7,ey+4,'shadow',3);line(sh,ex+7,ey+4,tx+4,ty-5,'shadow',3)
 for k in range(1,9):
  q=k/9;rect(sh,bx+(ex-bx)*q+5,by+(ey-by)*q+3,1.5,1.5,'coral')
 geometry(mechanics,sh)
 sh=[]
 if not ending and 6.7<=t<7:rect(sh,10,13,round((t-6.7)/.3*16),1.5,'cyan')
 elif ending or t>=7:
  blink=7.1<t<7.25 or 9.3<t<9.45
  for xx in (10,22):
   if blink:rect(sh,xx,10,4,1,'cyan')
   elif ending or t>=7.3:line(sh,xx,10,xx+2,8,'cyan');line(sh,xx+2,8,xx+4,10,'cyan')
   else:rect(sh,xx,8,4,3,'cyan');rect(sh,xx,8,1,1,'white')
  line(sh,15,14,18,15,'teal');line(sh,18,15,21,14,'teal')
 geometry(face,sh);place(face,hx,hy)
 # The spare head arrives before the boundary; first/last poses agree.
 next_head.hide_render=ending or t<10.2
 sh=[]
 if not ending and t>=10.2:
  supply_x=mix(530,349,(t-10.2)/.9);pose(next_head,supply_x-3,124,40,36)
  cart_x=supply_x if t<11.5 else mix(349,530,(t-11.5)/.5)
  box(sh,cart_x+12,116,10,6,'steel','mid');line(sh,cart_x+17,122,cart_x+17,132,'light')
 geometry(shuttle,sh)
 sh=[]
 if not ending and 0<t<3.2:
  for xx in (x-1,x+31):line(sh,xx,184,xx,184-33*p,'rust',.5)
  for i in range(3):rect(sh,nx-3+i*3,tip_y+1+(frame+i)%3,.5,.5,'amber')
 elif not ending and 3.2<=t<5.2:
  box(sh,x-6,body_y+33,42,2,'steel','light');rect(sh,x-4,body_y+34,38,.5,'orange')
 geometry(sparks,sh)
 # A second illustrated sprite character watches the line and waves periodically.
 bob=math.sin(clock*math.tau/3)*.5
 pose(helper['head'],71,228+bob,26,24)
 pose(helper['torso'],76,251+bob,16,14)
 pose(helper['left leg'],78,263,4.5,8.5);pose(helper['right leg'],85,263,4.5,8.5)
 pose(helper['left arm'],71,253+bob,5,13)
 helper_wave=(.6+.25*math.sin(clock*math.tau/3))*(1-smooth((t-4)/2) if ending else 1)
 pose(helper['right arm'],92,253+bob,5,13,angle=helper_wave)
 sh=[]
 for xx in (79,87):line(sh,xx,243+bob,xx+1,242+bob,'cyan');line(sh,xx+1,242+bob,xx+2,243+bob,'cyan')
 geometry(helper_face,sh)
 sh=[]
 if season_parts:
  witch=opts.season=='halloween';hat_w,hat_h=(39,27) if witch else (34,23)
  pose(season_parts['hero-hat'],hx-3,hy-12-hat_h+15,hat_w,hat_h)
  pose(season_parts['helper-hat'],71,228+bob-hat_h*.62+11,hat_w*.66,hat_h*.62)
  season_parts['supply-hat'].hide_render=ending or t<10.2
  if not ending and t>=10.2:pose(season_parts['supply-hat'],supply_x-3,124-hat_h+15,hat_w,hat_h)
  if witch:
   pose(season_parts['prop'],460+math.sin(clock*math.tau/12)*8,174+math.sin(clock*math.tau/3)*4,19,19)
  else:
   # A parcel follows the moving line; it exits completely before its periodic reset.
   parcel_x=-28+(clock/12)*568 if not ending else mix(345,540,t/6)
   pose(season_parts['prop'],parcel_x,194,20,20)
   pose(season_parts['scarf'],hx+2,hy+21,25,13)
  # Small hanging lights are independent geometry, outside the editable sign.
  for i in range(14):
   xx=30+i*35;yy=7+(i%3)*2
   color=('orange' if i%2 else 'coral') if witch else ('coral' if i%2 else 'cyan')
   lit=(i+int(clock*4))%4<2
   rect(sh,xx,yy,1.5,2.5,color if lit else 'steel_dark')
 geometry(ambient,sh)

animate(0)
if opts.save:bpy.ops.wm.save_as_mainfile(filepath=str(ROOT/f'{opts.season}-{opts.stage}.blend'))
frames=opts.frames if opts.frames is not None else [opts.still] if opts.still is not None else range(288)
for frame in frames:
 s.frame_set(frame);animate(frame);s.render.filepath=str(OUT/f'{frame:04}.png');bpy.ops.render.render(write_still=True)
