import letters from './font.json' with {type:'json'};
import {drawFrame as original} from './original.mjs';
// Throwaway visual question: compare the original with an overhead maze machine
// and a physical cabinet. Real motion, crisp pixels, and a protected message.
export const WIDTH=960, HEIGHT=540, DURATION=32, FPS=30;
const ghostColors=['#ff5268','#ff9bdd','#43d7e9','#ffb35a'];
const clamp=(v,a,b)=>Math.max(a,Math.min(b,v));
const mix=(a,b,n)=>'#'+[1,3,5].map(i=>Math.round(parseInt(a.slice(i,i+2),16)*(1-n)+parseInt(b.slice(i,i+2),16)*n).toString(16).padStart(2,'0')).join('');
const wrap=(n,m)=>((n%m)+m)%m;
function brush(ctx) {return (x,y,w,h,c)=>{ctx.fillStyle=c;ctx.fillRect(Math.round(x),Math.round(y),Math.round(w),Math.round(h));};}
function lettering(r,text,x,y,size,color,shadow=true) {
 for(const [i,ch] of [...text.toUpperCase()].entries()) for(const [dy,row] of (letters[ch]||[]).entries()) for(let dx=0;dx<row.length;dx++) if(row[dx]==='1') {
  if(shadow)r(x+(i*6+dx)*size+size/2,y+dy*size+size,size,size,'#06111e');
  r(x+(i*6+dx)*size,y+dy*size,size,size,color);
 }
}
function centred(r,text,y,size,color) {lettering(r,text,(WIDTH-(text.length*6-1)*size)/2,y,size,color);}
function orb(r,x,y,radius,color) {for(let yy=-radius;yy<=radius;yy++){const ww=Math.floor(Math.sqrt(radius*radius-yy*yy));r(x-ww,y+yy,ww*2+1,1,color);}}
function ghost(r,x,y,color,t,dir,frightened=false,eyesOnly=false) {
 x=Math.round(x);y=Math.round(y+Math.sin(t*Math.PI*2+x*.01));
 const body=frightened?'#5069f1':color;
 if(!eyesOnly){
  for(let dy=-15;dy<15;dy++)for(let dx=-14;dx<=14;dx++){
   if(dy<0&&dx*dx+(dy+1)*(dy+1)>212)continue;
   if(dy>10&&wrap(dx+Math.floor(t*8)%2*4,9)<4)continue;
   const edge=Math.abs(dx)>11||dy===-14||dy>12;
   const shade=edge?mix(body,'#09162b',.55):mix(body,dx<0?'#ffffff':'#101341',dx<0?clamp((-dx-dy)*.016,0,.42):clamp(dx*.038,0,.45));
   r(x+dx,y+dy,1,1,shade);
  }
  r(x-9,y-8,3,2,mix(body,'#ffffff',.7));
 }
 if(frightened&&!eyesOnly){
  const c=t>14&&Math.floor(t*8)%2?'#fff6d6':'#dbe8ff';r(x-7,y-5,3,4,c);r(x+4,y-5,3,4,c);
  for(let dx=-7;dx<8;dx++)r(x+dx,y+5+(Math.floor((dx+7)/2)%2)*2,1,2,c);
 }else for(const dx of [-7,6]){
  r(x+dx-4,y-7,8,10,'#879eaf');r(x+dx-3,y-7,7,9,'#fbffef');
  r(x+dx-1+dir[0]*2,y-4+dir[1]*2,3,5,'#173f7f');r(x+dx+dir[0]*2,y-4+dir[1]*2,1,2,'#42b8ff');
 }
}
function pacman(r,x,y,t,dir,powered) {
 const mouth=[.05,.35,.65,.9,.65,.35][Math.floor(t*12)%6];
 for(let dy=-16;dy<=16;dy++)for(let dx=-16;dx<=16;dx++){
  const d=dx*dx+dy*dy,forward=dx*dir[0]+dy*dir[1],side=dx*dir[1]-dy*dir[0];
  if(d>250||forward>=0&&Math.abs(side)<forward*mouth)continue;
  let c=d>205?'#cf771f':mix('#ffda3c',dy<-3?'#fff3a1':'#f3981e',dy<-3?clamp((-dy-dx)*.035,0,.6):clamp((dy+dx)*.022,0,.65));
  if(powered&&d>224)c='#fff4bb';r(x+dx,y+dy,1,1,c);
 }
 const ex=x+dir[0]*2+dir[1]*7,ey=y+dir[1]*2-dir[0]*7;r(ex-1,ey-1,3,3,'#39291c');
 r(x-8,y-9,4,2,'#fff6b5');
}
function pointOnTrack(d,top,bottom,left=76,right=884) {
 const w=right-left,h=bottom-top,L=2*(w+h);d=wrap(d,L);
 if(d<w)return {x:left+d,y:top,dir:[1,0]};d-=w;
 if(d<h)return {x:right,y:top+d,dir:[0,1]};d-=h;
 if(d<w)return {x:right-d,y:bottom,dir:[-1,0]};d-=w;
 return {x:left,y:bottom-d,dir:[0,-1]};
}
function fan(r,x,y,radius,t) {
 orb(r,x,y,radius,'#081727');
 for(let i=0;i<5;i++)for(let d=4;d<radius-1;d++){
  const a=t*Math.PI/2+i*Math.PI*2/5+d*.045;
  r(x+Math.cos(a)*d,y+Math.sin(a)*d,2,2,d%3===0?'#617d87':'#324b60');
 }
 orb(r,x,y,3,'#a57938');r(x-1,y-2,2,1,'#e4c886');
}
export function drawFrame(ctx,seconds,text='BE RIGHT BACK',variant='B',assets={}) {
 const t=wrap(seconds,DURATION),r=brush(ctx);
 if(variant==='A'){ctx.save();ctx.scale(3,3);original(ctx,t,text);ctx.restore();return;}
 const cabinet=variant==='C',top=cabinet?112:98,bottom=cabinet?350:408,left=cabinet?125:76,right=cabinet?835:884;
 const py=cabinet?177:183,ph=cabinet?108:143,track=(d)=>pointOnTrack(d,top,bottom,left,right);
 const length=2*(right-left+bottom-top),speed=length/16,powered=t>=8&&t<24,dir=powered?-1:1;
 const progress=t<8?t/16:t<24?.5-(t-8)/16:-.5+(t-24)/16,d=progress*length;
 ctx.imageSmoothingEnabled=false;
 if(!assets.maze || !assets.cabinet)throw new Error('Scenery assets must be loaded before drawing.');
 if(cabinet){
  ctx.drawImage(assets.cabinet,0,0,960,540);
  const im=assets.maze;
  ctx.drawImage(im,im.width*55/960,im.height*75/540,im.width*850/960,im.height*355/540,106,95,748,270);
  centred(r,'PLAYER ONE / INTERMISSION',32,3,'#603013');
  for(const x of [128,831])for(let i=0;i<10;i++){
   const a=t*Math.PI/2+i*Math.PI/5;
   r(x+Math.cos(a)*20,41+Math.sin(a)*20,1,2,'#e7a54c');
  }
 }else{
  ctx.drawImage(assets.maze,0,0,960,540);
  fan(r,102,488,21,t);fan(r,314,23,12,-t);
 }
 // Dots and power pellets live on the same path as the moving actors.
 for(let i=0;i<Math.floor(length/24);i++){
  const pd=i*24,p=track(pd),distance=wrap((pd-d)*dir,length);
  if(distance<length*.77){r(p.x-2,p.y-2,4,4,'#26344b');r(p.x-1,p.y-2,3,3,'#efcc9e');r(p.x-1,p.y-2,1,1,'#fff3d3');}
 }
 if(!powered){const p=track(length/2);orb(r,p.x,p.y,8,'#243454');orb(r,p.x,p.y,5,Math.floor(t*4)%2?'#fff3be':'#f5b656');r(p.x-2,p.y-3,2,2,'#ffffff');}
 // Text is drawn on the plate's empty glass sign, never baked into an image.
 const clean=text.toUpperCase(),available=cabinet?310:374,size=Math.min(5,Math.max(1,Math.floor(available/Math.max(1,clean.length*6-1))));
 centred(r,clean,py+Math.floor((ph-7*size)/2)-2,size,'#fff0c8');
 centred(r,'STREAM PAUSED',py+ph-20,1,'#588eaa');
 // Native sprite pixels scale as whole rectangles, avoiding blurred character edges.
 const sprite=(x,y)=> (sx,sy,w,h,c)=>r(x+(sx-x)*.8,y+(sy-y)*.8,Math.max(1,w*.8),Math.max(1,h*.8),c);
 const shadow=(x,y)=>{r(x-11,y+12,23,2,'#061329');};
 const phase=powered?t-8:0,houseY=py+ph+19;
 ghostColors.forEach((color,i)=>{
  const gap=56*(i+1),catchTime=gap/(speed*.42);
  if(powered&&phase>catchTime){
   const caughtD=.5*length-catchTime*speed,caught=track(caughtD),age=phase-catchTime;
   const homeD=(right-left)+(bottom-top)+(right-left)/2;
   let delta=wrap(homeD-caughtD,length);if(delta>length/2)delta-=length;
   if(age<1.5){const p=track(caughtD+delta*age/1.5);ghost(sprite(p.x,p.y),p.x,p.y,color,t,p.dir.map(v=>v*Math.sign(delta)),false,true);}
   else if(age<2.1){const u=(age-1.5)/.6,x=480+(i-1.5)*13*u,y=bottom+(houseY-bottom)*u;ghost(sprite(x,y),x,y,color,t,[0,-1],false,true);}
   else{const gx=459+i*14;for(let q=0;q<2;q++){r(gx+q*5,houseY,3,4,'#d6e6e4');r(gx+q*5+1,houseY+1,1,2,'#4284ff');}}
   if(age<.75)lettering(r,String(200*2**i),caught.x-10,caught.y-21-age*12,1,'#9eeaff');
  }else{
   const offset=powered?-(gap-phase*speed*.42):-gap,p=track(d+offset);shadow(p.x,p.y);ghost(sprite(p.x,p.y),p.x,p.y,color,t,p.dir.map(v=>v*dir),powered);
  }
 });
 const player=track(d);player.dir=player.dir.map(v=>v*dir);shadow(player.x,player.y);pacman(sprite(player.x,player.y),player.x,player.y,t,player.dir,powered);
 // Moving light packets remain on the existing luminous outer rail.
 for(let i=0;i<3;i++){
  const p=track(d+i*length/3),pulse=powered?'#fff1bb':'#b3f9ff';
  if(Math.abs(p.y-top)<1)r(p.x-4,top-(cabinet?16:20),8,2,pulse);
  if(Math.abs(p.y-bottom)<1)r(p.x-4,bottom+(cabinet?13:16),8,2,pulse);
 }
 // Sparse cabinet lamps brighten without painting rectangular halos over artwork.
 if(cabinet){for(const [x,y] of [[69,220],[891,218],[356,424],[605,424]]){const b=.2+.4*(1+Math.sin(t*Math.PI/2+x));r(x,y,2,6,mix('#935819','#ffe69a',b));}}
}
export function phaseAt(t){t=wrap(t,32);return t<8?'Ghost chase':t<15?'Power pellet / reversal':t<24?'Returning eyes / recharge':'Chase resumes';}
