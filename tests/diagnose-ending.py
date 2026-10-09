"""Compare final-frame decoding of one captured MP4; never changes tolerances.
Usage: python3 diagnose.py INPUT.mp4 OUTPUT_DIRECTORY [DOCKER_CONTAINER]
All paths/artifacts are synthetic smoke data. ffmpeg/ffprobe are resolved on PATH.
"""
import collections
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys

source=Path(sys.argv[1]).resolve()
out=Path(sys.argv[2]).resolve();out.mkdir(parents=True,exist_ok=True)
shutil.copyfile(source,out/'ending.mp4')
decoders=[('host',[],str(source))]
if len(sys.argv)>3:
    container=sys.argv[3]
    target='/data/restreamer-ending-diagnostic.mp4'
    subprocess.run(['docker','cp',str(source),container+':'+target],check=True)
    decoders.append(('packaged',['docker','exec',container],target))

def stats(data,expected):
    values=collections.Counter(data)
    return dict(min=min(data),max=max(data),expected=expected,
                differing_samples=sum(n for v,n in values.items() if v!=expected),
                histogram=sorted(values.items()),sha256=hashlib.sha256(data).hexdigest())

results={}
for label,prefix,input_path in decoders:
    results[label]={}
    for binary in ('ffmpeg','ffprobe'):
        version=subprocess.run(prefix+[binary,'-version'],capture_output=True,text=True)
        (out/(label+'-'+binary+'-version.txt')).write_text(version.stdout+version.stderr)
        results[label][binary+'_version']=version.stdout.splitlines()[0] if version.stdout else version.stderr
    probe_command=prefix+['ffprobe','-v','error','-select_streams','v:0','-count_frames','-show_entries','stream=width,height,pix_fmt,color_range,color_space,color_transfer,color_primaries,nb_read_frames,start_time,duration','-of','json',input_path]
    probe=subprocess.run(probe_command,capture_output=True,text=True)
    (out/(label+'-probe.json')).write_text(probe.stdout)
    if probe.returncode:
        results[label]['probe_error']=probe.stderr
        continue
    stream=json.loads(probe.stdout)['streams'][0]
    results[label]['stream']=stream
    width,height=int(stream['width']),int(stream['height'])
    final=int(stream['nb_read_frames'])-1
    for name,pixfmt,w,h,scale in [('yuv','yuv420p',width,height,''),('rgb','rgb24',width,height,''),('scaled-rgb','rgb24',16,16,',scale=16:16')]:
        command=prefix+['ffmpeg','-v','error','-xerror','-i',input_path,'-map','0:v:0','-vf',f'select=eq(n\\,{final})'+scale,'-vsync','0','-frames:v','1','-pix_fmt',pixfmt,'-f','rawvideo','pipe:1']
        decoded=subprocess.run(command,capture_output=True)
        (out/(label+'-'+name+'.command.json')).write_text(json.dumps(command))
        (out/(label+'-'+name+'.raw')).write_bytes(decoded.stdout)
        (out/(label+'-'+name+'.stderr.txt')).write_bytes(decoded.stderr)
        data=decoded.stdout
        expected_size=w*h*(3 if pixfmt=='rgb24' else 1.5)
        if decoded.returncode or len(data)!=expected_size:
            results[label][name]={'error':decoded.stderr.decode(errors='replace'),'bytes':len(data),'expected_bytes':expected_size}
            continue
        if pixfmt=='rgb24':
            planes={key:stats(data[i::3],0) for i,key in enumerate('RGB')}
        else:
            n=w*h
            planes={'Y':stats(data[:n],16),'U':stats(data[n:n+n//4],128),'V':stats(data[n+n//4:],128)}
        results[label][name]=planes
    (out/'results.json').write_text(json.dumps(results,indent=2))
print('[ENDING-DIAGNOSTIC] '+json.dumps(results,sort_keys=True))
