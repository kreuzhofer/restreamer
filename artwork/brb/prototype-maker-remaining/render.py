"""Render and verify the two throwaway motion studies; no production mutation."""
import argparse
import hashlib
import json
import shutil
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--blender', default='/Applications/Blender.app/Contents/MacOS/Blender')
parser.add_argument('--ffmpeg', default='ffmpeg')
parser.add_argument('--ffprobe', default='ffprobe')
parser.add_argument('--variant', choices=['midnight', 'debug'])
parser.add_argument('--encode-only', action='store_true', help='Use already rendered frames, including inspection endpoint 384')
args = parser.parse_args()


def run(command):
    subprocess.run(command, check=True)


def pixels(path):
    return subprocess.check_output([args.ffmpeg, '-v', 'error', '-i', str(path),
                                    '-f', 'rawvideo', '-pix_fmt', 'rgb24', '-'])


def difference(a, b):
    return {'mean_absolute_channel_difference': round(sum(abs(x-y) for x, y in zip(a, b))/len(a), 6),
            'changed_pixel_fraction': round(sum(a[i:i+3] != b[i:i+3] for i in range(0, len(a), 3))/(len(a)/3), 6)}


public = ROOT/'public'
public.mkdir(exist_ok=True)
report_path = ROOT/'verification.json'
report = json.loads(report_path.read_text()) if report_path.exists() else {}
for variant in [args.variant] if args.variant else ['midnight', 'debug']:
    if not args.encode_only:
        run([args.blender, '-b', '--python', str(ROOT/'build.py'), '--', '--variant', variant, '--save'])
        run([args.blender, '-b', '--python', str(ROOT/'build.py'), '--', '--variant', variant, '--frames', '384'])
    frames = ROOT/'renders'/variant
    for frame in range(385):
        if not (frames/f'{frame:04d}.png').is_file():
            raise SystemExit(f'Missing {variant} frame {frame}; render before encoding')
    first, previous, last, endpoint = [pixels(frames/f'{frame:04d}.png') for frame in [0, 382, 383, 384]]
    if first != endpoint:
        raise SystemExit(f'{variant}: source endpoint does not equal frame zero')
    movie = public/f'{variant}.mp4'
    run([args.ffmpeg, '-v', 'error', '-y', '-framerate', '24', '-i', str(frames/'%04d.png'),
         '-frames:v', '384', '-vf', 'scale=2048:1152:flags=neighbor', '-c:v', 'libx264',
         '-threads', '2', '-crf', '16', '-pix_fmt', 'yuv420p', '-movflags', '+faststart', str(movie)])
    probe = json.loads(subprocess.check_output([args.ffprobe, '-v', 'error', '-count_frames',
                       '-show_streams', '-of', 'json', str(movie)]))['streams'][0]
    if (probe['width'], probe['height'], probe['r_frame_rate'], int(probe['nb_read_frames']), float(probe['duration'])) != (2048, 1152, '24/1', 384, 16):
        raise SystemExit(f'{variant}: unexpected encoded video metadata')
    run([args.ffmpeg, '-v', 'error', '-xerror', '-i', str(movie), '-f', 'null', '-'])
    shutil.copyfile(frames/'0120.png', public/f'{variant}.png')
    report[variant] = {'frames': 384, 'fps': 24, 'duration_seconds': 16, 'dimensions': [2048, 1152],
                       'source_endpoint_matches_start': True, 'decode': 'success',
                       'preceding_transition': difference(previous, last), 'loop_transition': difference(last, first),
                       'video_sha256': hashlib.sha256(movie.read_bytes()).hexdigest()}
report_path.write_text(json.dumps(report, indent=2)+'\n')
print(json.dumps(report, indent=2))
