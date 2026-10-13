// Offline rendering of the same throwaway Canvas scene. FFmpeg decodes the two
// plates; this tiny raster adapter implements only the drawing operations used.
import {spawn,execFileSync} from 'node:child_process';
import {once} from 'node:events';
import {readFile,writeFile} from 'node:fs/promises';
import {fileURLToPath} from 'node:url';
import {createHash} from 'node:crypto';
import {drawFrame,WIDTH,HEIGHT,FPS,DURATION} from './animation.mjs';
const dir=fileURLToPath(new URL('.',import.meta.url)),ffmpeg=process.env.FFMPEG||'ffmpeg',probe=process.env.FFPROBE||'ffprobe';
function raster(){
 const pixels=Buffer.alloc(WIDTH*HEIGHT*3),colors=new Map();let sx=1,sy=1,stack=[];
 return {pixels,fillStyle:'#000000',save(){stack.push([sx,sy]);},restore(){[sx,sy]=stack.pop();},scale(x,y){sx*=x;sy*=y;},
  fillRect(x,y,w,h){
   x=Math.round(x*sx);y=Math.round(y*sy);w=Math.round(w*sx);h=Math.round(h*sy);
   const x0=Math.max(0,x),y0=Math.max(0,y),x1=Math.min(WIDTH,x+w),y1=Math.min(HEIGHT,y+h);if(x0>=x1||y0>=y1)return;
   if(!colors.has(this.fillStyle)){const rgb=Buffer.from([1,3,5].map(i=>parseInt(this.fillStyle.slice(i,i+2),16))),row=Buffer.alloc(WIDTH*3);row.fill(rgb);colors.set(this.fillStyle,row);}
   const row=colors.get(this.fillStyle).subarray(0,(x1-x0)*3);for(let yy=y0;yy<y1;yy++)pixels.set(row,(yy*WIDTH+x0)*3);
  },
  drawImage(im,...a){
   let ix=0,iy=0,iw=im.width,ih=im.height,dx,dy,dw,dh;
   if(a.length===4)[dx,dy,dw,dh]=a;else[ix,iy,iw,ih,dx,dy,dw,dh]=a;
   for(let y=0;y<dh;y++)for(let x=0;x<dw;x++){
    const px=Math.floor(dx+x),py=Math.floor(dy+y);if(px<0||px>=WIDTH||py<0||py>=HEIGHT)continue;
    const source=(Math.min(im.height-1,Math.floor(iy+(y+.5)*ih/dh))*im.width+Math.min(im.width-1,Math.floor(ix+(x+.5)*iw/dw)))*3,dest=(py*WIDTH+px)*3;
    pixels[dest]=im.pixels[source];pixels[dest+1]=im.pixels[source+1];pixels[dest+2]=im.pixels[source+2];
   }
  }
 };
}
const assets={};for(const name of ['maze','cabinet']){
 const path=dir+'assets/'+name+'.png',meta=JSON.parse(execFileSync(probe,['-v','error','-show_entries','stream=width,height','-of','json',path])).streams[0];
 assets[name]={...meta,pixels:execFileSync(ffmpeg,['-v','error','-i',path,'-pix_fmt','rgb24','-f','rawvideo','-'],{maxBuffer:32*1024*1024})};
}
for(const variant of ['B','C']){
 const canvas=raster();drawFrame(canvas,0,'BE RIGHT BACK',variant,assets);const start=Buffer.from(canvas.pixels);drawFrame(canvas,DURATION,'BE RIGHT BACK',variant,assets);if(!start.equals(canvas.pixels))throw Error('Loop endpoints differ');
 const out=dir+variant+'.mp4',child=spawn(ffmpeg,['-y','-v','error','-f','rawvideo','-pixel_format','rgb24','-video_size',`${WIDTH}x${HEIGHT}`,'-framerate',String(FPS),'-i','pipe:0','-vf','scale=1920:1080:flags=neighbor','-an','-c:v','libx264','-preset','fast','-crf','16','-pix_fmt','yuv420p','-threads','2','-movflags','+faststart',out],{stdio:['pipe','inherit','inherit']});
 const finished=once(child,'close');finished.catch(()=>{});child.stdin.on('error',()=>{});
 for(let i=0;i<FPS*DURATION;i++){
  drawFrame(canvas,i/FPS,'BE RIGHT BACK',variant,assets);
  if(!child.stdin.write(Buffer.from(canvas.pixels)))await Promise.race([once(child.stdin,'drain'),finished.then(([code])=>{throw Error('Encoder exited early '+code);})]);
 }
 child.stdin.end();const [code]=await finished;if(code!==0)throw Error('Encoder failed '+code);
 const metadata=JSON.parse(execFileSync(probe,['-v','error','-count_frames','-select_streams','v:0','-show_entries','stream=width,height,nb_read_frames,r_frame_rate','-of','json',out])).streams[0];
 if(metadata.width!==1920||metadata.height!==1080||metadata.nb_read_frames!==String(FPS*DURATION)||metadata.r_frame_rate!=='30/1')throw Error('Wrong video contract');
 execFileSync(ffmpeg,['-v','error','-xerror','-i',out,'-f','null','-']);
 execFileSync(ffmpeg,['-y','-v','error','-ss','3','-i',out,'-frames:v','1',dir+variant+'.png']);
 console.log(variant,metadata,'exact endpoint and full decode verified');
}
const names=['animation.mjs','original.mjs','font.json','render.mjs','preview.html','assets/maze.png','assets/cabinet.png','assets/prompts.json','B.mp4','C.mp4'];
const hashes={};for(const name of names)hashes[name]=createHash('sha256').update(await readFile(dir+name)).digest('hex');
await writeFile(dir+'manifest.json',JSON.stringify({width:1920,height:1080,fps:FPS,seconds:DURATION,hashes},null,2)+'\n');
