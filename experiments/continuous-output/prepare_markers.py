#!/usr/bin/env python3
"""Make a local synthetic/artwork fixture with input-side A/V sync markers."""
import argparse
import pathlib
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--artwork', type=pathlib.Path)
    parser.add_argument('--output', type=pathlib.Path, required=True)
    args = parser.parse_args()
    if args.output.exists() or not args.output.parent.is_dir() or args.artwork and not args.artwork.is_file():
        parser.error('use existing artwork (if selected) and a new output file')
    inputs = (['-protocol_whitelist', 'file,pipe', '-stream_loop', '-1', '-i', str(args.artwork)]
              if args.artwork else ['-f', 'lavfi', '-i', 'testsrc2=size=1920x1080:rate=25'])
    filters = 'scale=1920:1080,fps=25'
    if not args.artwork:
        filters += ',noise=alls=8:allf=t+u'
    filters += (',drawbox=x=iw-32:y=ih-32:w=32:h=32:color=black:t=fill'
                ",drawbox=x=iw-32:y=ih-32:w=32:h=32:color=white:t=fill:enable='eq(mod(n,25),0)'")
    audio = "aevalsrc=exprs='0.1*sin(2*PI*880*t)|if(lt(mod(t,1),0.01),0.7*sin(2*PI*2000*t),0)':s=48000:c=stereo"
    subprocess.run(['ffmpeg', '-hide_banner', '-v', 'error', '-nostdin', '-n', '-threads', '2'] + inputs +
                   ['-f', 'lavfi', '-i', audio, '-map', '0:v:0', '-map', '1:a:0',
                    '-t', '16', '-filter_threads', '1', '-vf', filters,
                    '-c:v', 'libx264', '-threads', '2', '-preset', 'veryfast', '-b:v', '12M',
                    '-pix_fmt', 'yuv420p', '-g', '50', '-c:a', 'aac', '-b:a', '192k', '-ac', '2',
                    '-ar', '48000', str(args.output)], check=True, timeout=180)


if __name__ == '__main__':
    main()
