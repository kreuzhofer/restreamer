import {spawn} from 'node:child_process';
import {once} from 'node:events';
import {createHash} from 'node:crypto';
import {readFile, writeFile, mkdir, mkdtemp, rename, rm} from 'node:fs/promises';
import {fileURLToPath} from 'node:url';
import {join} from 'node:path';
import letters from './font.json' with {type:'json'};
import {drawFrame, WIDTH, HEIGHT, FPS, DURATION, palette} from './animation.mjs';
import {rasterCanvas} from './raster.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url));
const output=join(root,'internal/relay/artwork');
const sources=['artwork/brb/animation.mjs','artwork/brb/raster.mjs','artwork/brb/render.mjs','artwork/brb/font.json'];
const assets=['internal/relay/artwork/arcade.mkv','internal/relay/artwork/poster.png','internal/relay/artwork/text.json'];
async function hashes(paths){const result={};for(const path of paths)result[path]=createHash('sha256').update(await readFile(join(root,path))).digest('hex');return result;}
if(process.argv.includes('--check')) {
  const manifest=JSON.parse(await readFile(join(output,'manifest.json'),'utf8'));
  if(JSON.stringify(manifest.hashes)!==JSON.stringify(await hashes([...sources,...assets]))) throw new Error('BRB artwork is stale or changed: run make brb-artwork');
  console.log('BRB artwork matches its editable source and recorded assets.');
} else {
  await mkdir(output,{recursive:true});
  const temp=await mkdtemp(join(output,'.render-'));
  async function encode(path,frames,args) {
    const child=spawn(process.env.FFMPEG || 'ffmpeg',['-hide_banner','-loglevel','error','-y','-f','rawvideo','-pixel_format','rgb24','-video_size',`${WIDTH}x${HEIGHT}`,'-framerate',String(FPS),'-i','pipe:0',...args,path],{stdio:['pipe','inherit','inherit']});
    const finished=once(child,'close');
    // Register rejection handling before the producer starts (e.g. missing FFmpeg).
    finished.catch(()=>{});
    // Capture EPIPE without an unhandled event; the exit status below is fatal.
    child.stdin.on('error',()=>{});
    try {
      const canvas=rasterCanvas(WIDTH,HEIGHT);
      for(let i=0;i<frames;i++) {
        drawFrame(canvas,frames===1?3:i/FPS,'');
        if(!child.stdin.write(Buffer.from(canvas.pixels))) await Promise.race([
          once(child.stdin,'drain'),
          finished.then(([code])=>{throw new Error(`FFmpeg stopped before rendering completed (${code})`);}),
        ]);
      }
      child.stdin.end();
      const [code]=await finished;
      if(code!==0) throw new Error(`FFmpeg render failed (${code})`);
    } catch(error) {child.kill();await finished.catch(()=>{});throw error;}
  }
  try {
    await encode(join(temp,'arcade.mkv'),FPS*DURATION,['-an','-c:v','libx264rgb','-crf','0','-preset','veryslow','-threads','2']);
    await encode(join(temp,'poster.png'),1,['-frames:v','1','-threads','1']);
    for(const name of ['arcade.mkv','poster.png']) await rename(join(temp,name),join(output,name));
    await writeFile(join(output,'text.json'),JSON.stringify({glyphs:letters,background:palette.background,foreground:palette.text,shadow:'#172a40'},null,2)+'\n');
    await writeFile(join(output,'manifest.json'),JSON.stringify({width:WIDTH,height:HEIGHT,fps:FPS,duration:DURATION,hashes:await hashes([...sources,...assets])},null,2)+'\n');
    console.log(`Rendered ${DURATION}s arcade loop at ${WIDTH}×${HEIGHT}, ${FPS} fps.`);
  } finally {await rm(temp,{recursive:true,force:true});}
}
