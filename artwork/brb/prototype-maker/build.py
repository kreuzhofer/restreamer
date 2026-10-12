"""THROWAWAY: Does a rendered maker sequence have convincing, readable action?
Blender background script; three compositions, one deterministic 12-second cycle.
Run: Blender -b --python build.py -- --variant A [--still 120]
"""
import bpy, math, json, sys, argparse, random
from pathlib import Path
from mathutils import Vector

args = argparse.ArgumentParser()
args.add_argument('--variant', default='A', choices=['A','B','C'])
args.add_argument('--still', type=int)
args.add_argument('--save', action='store_true')
args.add_argument('--style', default='miniature', choices=['miniature','pixel'])
opts = args.parse_args(sys.argv[sys.argv.index('--')+1:] if '--' in sys.argv else [])
ROOT = Path(__file__).resolve().parent
OUT = ROOT/'renders'; OUT.mkdir(exist_ok=True)
bpy.ops.object.select_all(action='SELECT'); bpy.ops.object.delete(use_global=False)
scene=bpy.context.scene
scene.render.engine='BLENDER_EEVEE'
scene.render.resolution_x=768; scene.render.resolution_y=432; scene.render.resolution_percentage=100
scene.render.image_settings.file_format='PNG'; scene.render.fps=24
scene.render.film_transparent=False
scene.world.color=(0.025,0.025,0.025)
scene.world.use_nodes=True
scene.world.node_tree.nodes['Background'].inputs['Color'].default_value=(.013,.022,.037,1)
scene.world.node_tree.nodes['Background'].inputs['Strength'].default_value=.35
scene.view_settings.view_transform='Standard'
scene.render.image_settings.color_mode='RGB'
scene.render.threads_mode='FIXED'; scene.render.threads=6
scene.frame_start=0; scene.frame_end=287

def mat(name, color, metal=0, emission=0):
 m=bpy.data.materials.new(name); m.diffuse_color=(*color,1); m.use_nodes=True
 p=m.node_tree.nodes.get('Principled BSDF'); p.inputs['Base Color'].default_value=(*color,1)
 p.inputs['Roughness'].default_value=.62; p.inputs['Metallic'].default_value=metal
 if emission:
  p.inputs['Emission Color'].default_value=(*color,1); p.inputs['Emission Strength'].default_value=emission
 return m
navy=mat('Deep ink',(0.022,.047,.085)); wall=mat('Workshop wall',(.037,.079,.125))
blue=mat('Painted steel',(.065,.15,.22),.25); edge=mat('Blue edges',(.12,.26,.35),.4)
dark=mat('Rubber',(.016,.025,.033)); silver=mat('Brushed metal',(.38,.48,.51),.65)
orange=mat('Safety amber',(.96,.36,.045),.15); coral=mat('Printed coral',(.94,.19,.075))
cream=mat('Ivory casing',(.8,.83,.69)); cyan=mat('Cyan pixels',(.11,.82,.76),0,1.2)
gold=mat('Warm light',(1,.64,.19),0,2); purple=mat('Violet',(.34,.17,.61))
black=mat('Screen glass',(.008,.02,.027)); white=mat('Lettering',(.97,.84,.55),0,.5)

# Second experiment: actual nearest-filtered pixel textures plus stepped shading.
# Geometry, animation and camera remain identical to the miniature reference.
if opts.style=='pixel':
 scene.render.resolution_x=512;scene.render.resolution_y=288
 for material in [navy,wall,blue,edge,dark,silver,orange,coral,cream,purple,black]:
  base=material.diffuse_color[:3];rng=random.Random(material.name)
  tex=bpy.data.images.new('Pixel texture - '+material.name,width=32,height=32)
  tex.colorspace_settings.name='Non-Color'
  pixels=[]
  for y in range(32):
   for x in range(32):
    # Deliberate small pixel clusters: sparse paint flecks, seams and scratches.
    value=1
    if x in [0,31] or y in [0,31]:value=.79
    elif (x//2+y//2)%13==0:value=1.12
    elif rng.random()<.045:value=.68
    if material==silver and y%8 in [0,1]:value=1.23
    pixels.extend([min(1,c*value) for c in base]+[1])
  tex.pixels=pixels;tex.pack()
  nodes=material.node_tree.nodes;links=material.node_tree.links;nodes.clear()
  output=nodes.new('ShaderNodeOutputMaterial');emission=nodes.new('ShaderNodeEmission')
  diffuse=nodes.new('ShaderNodeBsdfDiffuse');diffuse.inputs['Color'].default_value=(.8,.8,.8,1)
  to_rgb=nodes.new('ShaderNodeShaderToRGB');ramp=nodes.new('ShaderNodeValToRGB')
  ramp.color_ramp.interpolation='CONSTANT'
  ramp.color_ramp.elements.remove(ramp.color_ramp.elements[1])
  for i,(position,level) in enumerate([(0,.22),(.10,.46),(.26,.74),(.5,1.04)]):
   stop=ramp.color_ramp.elements[0] if i==0 else ramp.color_ramp.elements.new(position)
   stop.position=position;stop.color=(level,level,level,1)
  image=nodes.new('ShaderNodeTexImage');image.image=tex;image.interpolation='Closest';image.projection='BOX'
  geometry=nodes.new('ShaderNodeTexCoord');scale=nodes.new('ShaderNodeVectorMath');scale.operation='SCALE';scale.inputs[3].default_value=1.2
  multiply=nodes.new('ShaderNodeMixRGB');multiply.blend_type='MULTIPLY';multiply.inputs[0].default_value=1
  links.new(geometry.outputs['Object'],scale.inputs[0]);links.new(scale.outputs['Vector'],image.inputs['Vector'])
  links.new(diffuse.outputs[0],to_rgb.inputs[0]);links.new(to_rgb.outputs[0],ramp.inputs[0])
  links.new(image.outputs['Color'],multiply.inputs[1]);links.new(ramp.outputs[0],multiply.inputs[2])
  links.new(multiply.outputs[0],emission.inputs['Color']);links.new(emission.outputs[0],output.inputs['Surface'])

def cube(name, loc, size, material, bevel=0):
 bpy.ops.mesh.primitive_cube_add(size=1, location=loc); o=bpy.context.object; o.name=name
 o.dimensions=size; bpy.ops.object.transform_apply(location=False,rotation=False,scale=True)
 o.data.materials.append(material)
 if bevel:
  mod=o.modifiers.new('Machined edges','BEVEL'); mod.width=bevel; mod.segments=1
  o.modifiers.new('Weighted corner normals','WEIGHTED_NORMAL')
 return o
def cyl(name,loc,r,depth,material,axis='Z',vertices=12):
 bpy.ops.mesh.primitive_cylinder_add(vertices=vertices,radius=r,depth=depth,location=loc)
 o=bpy.context.object; o.name=name; o.data.materials.append(material)
 if axis=='Y':o.rotation_euler[0]=math.pi/2
 if axis=='X':o.rotation_euler[1]=math.pi/2
 return o
def beam(name,a,b,r,material):
 a,b=Vector(a),Vector(b); o=cyl(name,(a+b)/2,r,(b-a).length,material,vertices=8)
 o.rotation_euler=(b-a).to_track_quat('Z','Y').to_euler(); return o
def aim(o,point): o.rotation_euler=(Vector(point)-o.location).to_track_quat('-Z','Y').to_euler()
def light(name,loc,power,color,size):
 bpy.ops.object.light_add(type='AREA',location=loc); o=bpy.context.object;o.name=name
 o.data.energy=power;o.data.color=color;o.data.shape='DISK';o.data.size=size;aim(o,(0,0,1.5))
def pixeltext(text,x,y,z,scale,material):
 glyphs=json.loads((ROOT.parent/'font.json').read_text())
 verts=[]; faces=[]
 for i,ch in enumerate(text):
  for row,line in enumerate(glyphs.get(ch,[])):
   for col,v in enumerate(line):
    if v!='1':continue
    xx=x+(i*6+col)*scale; zz=z+(6-row)*scale; j=len(verts)
    verts.extend([(xx,y,zz),(xx+scale*.92,y,zz),(xx+scale*.92,y,zz+scale*.92),(xx,y,zz+scale*.92)])
    faces.append((j,j+1,j+2,j+3))
 mesh=bpy.data.meshes.new(text);mesh.from_pydata(verts,[],faces);mesh.materials.append(material)
 o=bpy.data.objects.new(text,mesh);bpy.context.collection.objects.link(o);return o

# A complete small set, not the generated concept used as a background.
cube('Floor',(0,1,-.23),(20,10,.4),navy)
cube('Back wall',(0,2.55,3.3),(20,.25,7),wall)
for x in range(-9,10,2):
 cube('Wall upright',(x,2.35,3.3),(.06,.12,6.6),blue)
for z in [1.8,3.7,6.3]:cube('Wall seam',(0,2.36,z),(20,.05,.035),edge)
for x in range(-9,10):cube('Floor seam',(x,0,-.014),(.012,8,.008),edge)
for y in range(-3,5):cube('Floor seam',(0,y,-.012),(20,.015,.008),edge)
cube('Message sign',(0,2.1,5.45),(12.4,.3,1.55),navy,.06)
pixeltext('BE RIGHT BACK',-5.43,1.92,5.02,.14,white)
for x in [-6.03,6.03]:
 cube('Sign end light',(x,1.91,5.45),(.055,.02,1.15),orange)
for x in [-7,7]:
 beam('Overhead cable',(x,2.1,6.4),(x,1.5,4.5),.025,dark)
 cube('Work lamp',(x,1.45,4.45),(1.1,.5,.13),blue,.04)
 cube('Lamp emitter',(x,1.45,4.37),(.85,.3,.02),gold)
light('Warm softbox',(-6,-7,11),1900,(1,.64,.35),7)
light('Cool fill',(5,-2,8),1400,(.3,.7,1),6)
light('Back rim',(2,5,9),1800,(.32,.8,1),5)

# Conveyor and an endless moving belt.
cube('Conveyor frame',(0,0,.72),(16,1.7,.32),blue,.07)
cube('Belt',(0,0,.92),(16,1.5,.13),dark)
for x in [-7,-3,1,5,7]:
 for y in [-.6,.6]:cube('Conveyor leg',(x,y,.3),(.18,.18,.65),edge)
for y in [-.82,.82]:cube('Conveyor rim',(0,y,.92),(16,.08,.22),silver)
slats=[]
for i in range(54):slats.append(cube('Belt slat',(i*.3-8,0,1.005),(.08,1.42,.025),edge))
for x in range(-7,8):
 cyl('Drive roller',(x,-.87,.74),.15,.1,dark,'Y')
 cyl('Roller bolt',(x,-.94,.74),.055,.04,silver,'Y',6)
for x in [-6,-2,2,6]:
 cube('Safety panel',(x,-.9,.65),(1.2,.025,.23),orange)
 for d in [-.4,0,.4]:
  stripe=cube('Hazard stripe',(x+d,-.921,.65),(.14,.025,.27),dark);stripe.rotation_euler[1]=-.45

# Printer: rails and moving nozzle are separate geometry.
px=-4.8
for x in [px-1.25,px+1.25]:
 for y in [-.64,.64]:cube('Printer upright',(x,y,2.3),(.13,.15,2.8),dark)
 cube('Printer rail',(x,-.7,2.4),(.055,.035,2.6),silver)
for y in [-.64,.64]:cube('Printer top',(px,y,3.71),(2.65,.18,.17),blue,.025)
cube('Printer bed',(px,0,1.13),(2.4,1.4,.1),silver)
cube('Bed surface',(px,0,1.193),(2.25,1.28,.025),navy)
gantry=cube('Moving X gantry',(px,0,1.4),(2.5,.14,.12),silver)
nozzle=cube('Hot end',(px,0,1.4),(.34,.4,.34),orange,.04)
tip=cyl('Nozzle tip',(px,0,1.2),.07,.15,silver)
spool=cyl('Filament',(px-1.7,.25,3.3),.48,.32,coral,'X',24)
for x in [px-1.9,px-1.5]:cyl('Spool rim',(x,.25,3.3),.54,.06,dark,'X',24)
cyl('Spool hub',(px-1.94,.25,3.3),.14,.08,silver,'X')
cube('Printer control',(px,-.88,1.42),(.86,.2,.38),blue,.025)
cube('Printer control screen',(px-.08,-.992,1.45),(.56,.016,.19),black)
pixeltext('PRINT',px-.32,-1.005,1.4,.025,cyan)
cyl('Control knob',(px+.3,-1,1.43),.065,.06,orange,'Y')

# CAD/AI station on a separate shelf.
cube('Terminal shelf',(-7.6,1.4,2.2),(2.2,1.4,.16),blue)
cube('Monitor',(-7.7,1.65,3.2),(1.7,.25,1.35),edge,.08)
cube('Monitor glass',(-7.7,1.51,3.2),(1.48,.025,1.13),black)
pixeltext('AI CAD',-8.29,1.48,3.56,.045,cyan)
wire=[]
for a,b in [((-.36,-.36),(.36,-.36)),((.36,-.36),(.36,.36)),((.36,.36),(-.36,.36)),((-.36,.36),(-.36,-.36))]:
 wire.append(beam('Wireframe',(-7.7+a[0],1.46,3.1+a[1]),(-7.7+b[0],1.46,3.1+b[1]),.015,cyan))
for i in range(3):cube('AI status pixel',(-8.23+i*.13,1.46,2.77),(.07,.02,.04),orange)
cube('Keyboard',(-7.6,.93,2.35),(1.3,.5,.08),dark)
for i in range(10):cube('Keyboard key',(-8.15+i*.12,.85,2.4),(.07,.08,.025),silver)

# Storage gives scale and depth without decorative animation.
for x in [3.6,6.6]:
 cube('Storage cabinet',(x,1.7,1.45),(2.0,1.2,2.6),blue,.05)
 for z in [.5,1.1,1.7,2.3]:
  cube('Drawer',(x,1.08,z),(1.8,.04,.48),edge,.02)
  cube('Drawer handle',(x,1.02,z),(.5,.08,.055),silver)
for i,color in enumerate([orange,cyan,purple,coral]):
 cyl('Stored spool',(3.05+i*.4,1.8,3.0),.22,.25,color,'X')
cube('Pegboard',(6.4,2.14,3.85),(2.5,.14,1.25),navy)
for i in range(6):
 beam('Tool handle',(5.5+i*.35,2.02,3.4),(5.5+i*.35,2.02,4.05),.035,silver)
 cube('Tool grip',(5.5+i*.35,2.02,3.54),(.11,.08,.3),orange if i%2 else cyan)

# Robot geometry is retained as local offsets; printing reveals each part by height.
parts=[]
def part(name,loc,size,material):
 o=cube(name,loc,size,material,.025);parts.append((o,Vector(loc),Vector(size)));return o
part('Body',(0,0,.64),(.68,.48,.67),coral)
part('Chest inset',(0,-.252,.67),(.43,.035,.32),orange)
part('Chest status',(0,-.276,.7),(.15,.02,.07),cyan)
for sign in [-1,1]:
 part('Leg',(sign*.21,0,.22),(.17,.21,.34),silver)
 part('Foot',(sign*.21,-.09,.055),(.29,.43,.11),dark)
 part('Arm',(sign*.47,0,.63),(.18,.23,.44),coral)
 part('Hand',(sign*.47,-.02,.38),(.2,.25,.18),cream)
headparts=[]
for name,loc,size,material in [
 ('Head',(0,0,0),(.88,.57,.64),cream),('Face',(0,-.298,0),(.68,.035,.43),black),
 ('Left eye',(-.16,-.323,.03),(.1,.016,.09),cyan),('Right eye',(.16,-.323,.03),(.1,.016,.09),cyan),
 ('Smile',(0,-.323,-.1),(.19,.016,.035),cyan),('Antenna',(0,0,.43),(.05,.05,.24),silver),
 ('Antenna light',(0,0,.56),(.12,.12,.09),orange)]:
 o=cube(name,loc,size,material,.018 if name=='Head' else 0);headparts.append((o,Vector(loc)))

# Articulated arm with explicit two-link inverse kinematics in X/Z.
base=Vector((.2,.86,1.32)); L1=1.6; L2=1.55
cyl('Arm pedestal',(.2,.86,.9),.42,1.1,blue)
shoulder=cyl('Shoulder',base,.23,.42,orange,'Y')
upper=cube('Upper arm',(0,0,0),(.24,.3,L1),orange,.045)
lower=cube('Forearm',(0,0,0),(.2,.26,L2),orange,.035)
elbow=cyl('Elbow',(0,0,0),.21,.38,dark,'Y')
wrist=cyl('Wrist',(0,0,0),.13,.33,silver,'Y')
grips=[cube('Gripper',(0,0,0),(.08,.16,.26),silver) for _ in range(2)]
def position_link(o,a,b):
 o.location=(a+b)/2;o.rotation_euler=(b-a).to_track_quat('Z','Y').to_euler()
def arm(x,z,opening):
 target=Vector((x,.86,z)); d=target-base; dist=max(.1,min(d.length,L1+L2-.001))
 angle=math.atan2(d.z,d.x); bend=math.acos((L1*L1+dist*dist-L2*L2)/(2*L1*dist))
 joint=base+Vector((math.cos(angle+bend)*L1,0,math.sin(angle+bend)*L1))
 position_link(upper,base,joint);position_link(lower,joint,target);elbow.location=joint;wrist.location=target
 for i,o in enumerate(grips):o.location=(x+(-1 if i==0 else 1)*opening,.1,z-.2)
 # Tool offset reaches forward over the center of the belt.
 return target
tool=cube('Tool reach',(0,0,0),(.19,.88,.16),blue)

def ease(v):v=max(0,min(1,v));return v*v*(3-2*v)
def mix(a,b,t):return a+(b-a)*ease(t)
def state(seconds):
 t=seconds%12
 if t<3.2:return 'PRINT',px,0
 if t<5.2:return 'TRANSFER',mix(px,.6,(t-3.2)/2),0
 if t<7.3:return 'ASSEMBLE',.6,0
 return 'FIRST STEPS',mix(.6,9.6,(t-7.3)/4.3),max(0,t-7.3)
def animate(seconds):
 t=seconds%12;phase,x,walking=state(t)
 for i,o in enumerate(slats):o.location.x=((i*.3+t*.9)%16.2)-8.1
 spool.rotation_euler[0]=t*.55
 printheight=1.01*ease((t-.15)/2.8) if t<3.2 else 1.1
 bottom=1.21 if t<3.2 else mix(1.21,1.03,(t-3.2)/.7)
 for o,offset,size in parts:
  extent=max(.0001,min(size.z,printheight-(offset.z-size.z/2)))
  o.hide_render=t<.15 or (t<3.2 and printheight<offset.z-size.z/2)
  o.scale.z=extent/size.z if t<3.2 else 1
  o.location=(x+offset.x,offset.y,bottom+offset.z-size.z/2+extent/2 if t<3.2 else bottom+offset.z)
  o.rotation_euler=(0,0,0)
  if walking:
   stride=math.sin(walking*11)
   if o.name.startswith(('Leg','Foot')):
    o.location.x+=stride*.13*(1 if offset.x>0 else -1);o.location.z+=max(0,stride*(1 if offset.x>0 else -1))*.08
   if o.name.startswith(('Arm','Hand')):o.location.z+=stride*.08*(1 if offset.x>0 else -1)
   o.location.z+=abs(math.sin(walking*11))*.035
 nozzle.location=(px+math.sin(t*19)*.32,math.sin(t*11)*.18,bottom+printheight+.24) if t<3.2 else (px-1,0,3.3)
 tip.location=(nozzle.location.x,nozzle.location.y,nozzle.location.z-.23)
 gantry.location.z=nozzle.location.z+.08
 if t<5.2: hx,hz=2.05,3.32
 elif t<6.65:hx,hz=mix(2.05,.6,(t-5.2)/1.45),mix(3.32,2.34,(t-5.2)/1.45)
 else:hx,hz=x,2.34+abs(math.sin(walking*11))*.035
 for o,offset in headparts:
  o.location=Vector((hx,0,hz))+offset;o.hide_render=False
 if t<6.7:ax,az=hx,hz+.54
 elif t<7.5:ax,az=mix(.6,2.05,(t-6.7)/.8),mix(2.88,3.86,(t-6.7)/.8)
 else:ax,az=2.05,3.86
 arm(ax,az,.47 if t<6.7 else .61)
 tool.location=(ax,.44,az)
 if opts.variant=='C':
  focus=Vector((max(-4.8,min(2,x)),0,1.85));cam.location=focus+Vector((6,-11,6.6));aim(cam,focus)

bpy.ops.object.camera_add();cam=bpy.context.object;scene.camera=cam;cam.data.type='ORTHO';cam.data.lens=45
if opts.variant=='A':cam.location=(0,-25,9.3);cam.data.ortho_scale=18.8;aim(cam,(0,.5,2.8))
if opts.variant=='B':cam.location=(12,-19,15);cam.data.ortho_scale=21;aim(cam,(0,.7,2.4))
if opts.variant=='C':cam.data.ortho_scale=10.2
if opts.save:
 animate(0);bpy.ops.wm.save_as_mainfile(filepath=str(ROOT/('maker-pixel-prototype.blend' if opts.style=='pixel' else 'maker-prototype.blend')))
render_key=opts.variant+('-pixel' if opts.style=='pixel' else '')
if opts.still is not None:
 animate(opts.still/24);scene.render.filepath=str(OUT/f'{render_key}-still.png');bpy.ops.render.render(write_still=True)
else:
 folder=OUT/render_key;folder.mkdir(exist_ok=True)
 for frame in range(288):
  animate(frame/24);scene.render.filepath=str(folder/f'{frame:04}.png');bpy.ops.render.render(write_still=True)
