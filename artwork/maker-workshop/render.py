"""Rebuild and verify prepared workshop media; Blender is authoring-only."""
import argparse, json, shutil, subprocess
from pathlib import Path

parser=argparse.ArgumentParser()
parser.add_argument('--blender',default='blender')
parser.add_argument('--ffmpeg',default='ffmpeg')
parser.add_argument('--ffprobe',default='ffprobe')
parser.add_argument('--season',choices=['workshop','halloween','christmas'])
parser.add_argument('--stage',choices=['brb','prestream','ending'])
args=parser.parse_args()
root=Path(__file__).resolve().parent
repo=root.parents[1]
for tool in (args.blender,args.ffmpeg,args.ffprobe):
 if not shutil.which(tool):raise SystemExit(f'Required authoring tool is unavailable: {tool}')
for season in ([args.season] if args.season else ['workshop','halloween','christmas']):
 for stage in ([args.stage] if args.stage else ['brb','prestream','ending']):
  print(f'Rendering {season}/{stage}',flush=True)
  subprocess.run([args.blender,'-b','--python',str(root/'build.py'),'--','--season',season,'--stage',stage],check=True)
  destination=repo/'internal'/'relay'/'artwork'/'maker-workshop'/season
  destination.mkdir(parents=True,exist_ok=True)
  frames=root/'renders'/season/stage
  if stage!='ending':
   subprocess.run([args.blender,'-b','--python',str(root/'build.py'),'--','--season',season,'--stage',stage,'--frames','288'],check=True)
  def rgb(frame):
   return subprocess.check_output([args.ffmpeg,'-v','error','-i',str(frames/f'{frame:04}.png'),'-pix_fmt','rgb24','-f','rawvideo','-'])
  if stage=='ending':
   if rgb(144)!=rgb(287):raise SystemExit(f'Ending does not settle: {season}')
  elif rgb(0)!=rgb(288):raise SystemExit(f'Loop endpoint differs from start: {season}/{stage}')
  video=destination/f'{stage}.mp4'
  subprocess.run([args.ffmpeg,'-y','-v','error','-framerate','24','-i',str(frames/'%04d.png'),'-frames:v','288','-vf','scale=2048:1152:flags=neighbor','-c:v','libx264','-threads','2','-crf','12','-pix_fmt','yuv420p','-movflags','+faststart',str(video)],check=True)
  shutil.copyfile(frames/'0000.png',destination/f'{stage}.png')
  info=json.loads(subprocess.check_output([args.ffprobe,'-v','error','-select_streams','v:0','-count_frames','-show_entries','stream=width,height,r_frame_rate,nb_read_frames,duration','-of','json',str(video)]))['streams'][0]
  if info!={'width':2048,'height':1152,'r_frame_rate':'24/1','duration':'12.000000','nb_read_frames':'288'}:raise SystemExit(f'Invalid master: {video}: {info}')
  subprocess.run([args.ffmpeg,'-v','error','-xerror','-i',str(video),'-f','null','-'],check=True)
  print(f'Verified {season}/{stage}',flush=True)
