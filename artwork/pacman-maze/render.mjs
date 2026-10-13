import {spawn,execFileSync} from 'node:child_process';
import {once} from 'node:events';
import {readFile,writeFile,mkdir,mkdtemp,rename,rm} from 'node:fs/promises';
import {fileURLToPath} from 'node:url';
import {join} from 'node:path';
import {createHash} from 'node:crypto';
import {drawFrame,WIDTH,HEIGHT,FPS,DURATION} from './animation.mjs';
import {raster} from './raster.mjs';
const root=fileURLToPath(new URL('../../',import.meta.url)),out=join(root,'internal/relay/artwork/pacman-maze');
const ffmpeg=process.env.FFMPEG||'ffmpeg',probe=process.env.FFPROBE||'ffprobe';
const sources=['animation.mjs','motion.test.mjs','raster.mjs','render.mjs','background.png','prompts.json'].map(n=>'artwork/pacman-maze/'+n);
sources.push('artwork/brb/font.json');
const assets=['brb.mp4','brb.png','loop.mp4','loop.png'].map(n=>'internal/relay/artwork/pacman-maze/'+n);
async function hashes(){const result={};for(const file of [...sources,...assets])result[file]=createHash('sha256').update(await readFile(join(root,file))).digest('hex');return result;}
if(process.argv.includes('--check')){
 const manifest=JSON.parse(await readFile(join(out,'manifest.json'),'utf8'));
 if(manifest.duration!==DURATION||manifest.fps!==FPS||JSON.stringify(manifest.hashes)!==JSON.stringify(await hashes()))throw Error('Pac-Man artwork changed: run make pacman-artwork');
 console.log('Pac-Man Maze sources and prepared masters match the manifest.');
}else{
 await mkdir(out,{recursive:true});const temp=await mkdtemp(join(out,'.render-'));
 try{
  const plate=join(root,'artwork/pacman-maze/background.png');
  const meta=JSON.parse(execFileSync(probe,['-v','error','-show_entries','stream=width,height','-of','json',plate])).streams[0];
  const scenery={maze:{...meta,pixels:execFileSync(ffmpeg,['-v','error','-i',plate,'-pix_fmt','rgb24','-f','rawvideo','-'],{maxBuffer:32*1024*1024})}};
  for(const stage of ['brb','loop']){
   const canvas=raster();drawFrame(canvas,0,'',scenery,stage);const start=Buffer.from(canvas.pixels);
   drawFrame(canvas,DURATION,'',scenery,stage);if(!start.equals(canvas.pixels))throw Error('Loop endpoints differ');
   const path=join(temp,stage+'.mp4');
   const child=spawn(ffmpeg,['-y','-v','error','-f','rawvideo','-pixel_format','rgb24','-video_size',`${WIDTH}x${HEIGHT}`,'-framerate',String(FPS),'-i','pipe:0','-vf','scale=1920:1080:flags=neighbor','-an','-c:v','libx264','-preset','fast','-crf','16','-pix_fmt','yuv420p','-threads','2','-movflags','+faststart',path],{stdio:['pipe','inherit','inherit']});
   const finished=once(child,'close');finished.catch(()=>{});child.stdin.on('error',()=>{});
   try{
    for(let i=0;i<FPS*DURATION;i++){
     drawFrame(canvas,i/FPS,'',scenery,stage);
     if(!child.stdin.write(Buffer.from(canvas.pixels)))await Promise.race([once(child.stdin,'drain'),finished.then(([code])=>{throw Error('Encoder exited early '+code);})]);
    }
    child.stdin.end();const [code]=await finished;if(code!==0)throw Error('Encoder failed '+code);
   }catch(error){child.kill();await finished.catch(()=>{});throw error;}
   const metadata=JSON.parse(execFileSync(probe,['-v','error','-count_frames','-select_streams','v:0','-show_entries','stream=width,height,nb_read_frames,r_frame_rate','-of','json',path])).streams[0];
   if(metadata.width!==1920||metadata.height!==1080||metadata.nb_read_frames!==String(FPS*DURATION)||metadata.r_frame_rate!=='30/1')throw Error('Wrong video contract');
   execFileSync(ffmpeg,['-v','error','-xerror','-i',path,'-f','null','-']);
   execFileSync(ffmpeg,['-y','-v','error','-i',path,'-frames:v','1',join(temp,stage+'.png')]);
   console.log(stage,metadata,'exact endpoint and full decode verified');
  }
  for(const name of ['brb.mp4','brb.png','loop.mp4','loop.png'])await rename(join(temp,name),join(out,name));
  await writeFile(join(out,'manifest.json'),JSON.stringify({width:1920,height:1080,source_width:WIDTH,source_height:HEIGHT,fps:FPS,duration:DURATION,hashes:await hashes()},null,2)+'\n');
 }finally{await rm(temp,{recursive:true,force:true});}
}
