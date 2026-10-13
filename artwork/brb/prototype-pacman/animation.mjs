import letters from './font.json' with {type:'json'};
import {drawFrame as original} from './original.mjs';
// Throwaway visual question: compare the original with an overhead maze machine
// and a physical cabinet. Real motion, crisp pixels, and a protected message.
export const WIDTH=960, HEIGHT=540, DURATION=40, FPS=30;
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
function ghost(r,x,y,color,t,dir,frightened=false,eyesOnly=false,bodyFraction=1) {
 x=Math.round(x);y=Math.round(y+Math.sin(t*Math.PI*2+x*.01));
 const body=frightened?'#5069f1':color;
 if(!eyesOnly){
  for(let dy=-15;dy<15;dy++)for(let dx=-14;dx<=14;dx++){
   if(dy<15-30*bodyFraction)continue;
   if(dy<0&&dx*dx+(dy+1)*(dy+1)>212)continue;
   if(dy>10&&wrap(dx+Math.floor(t*8)%2*4,9)<4)continue;
   const edge=Math.abs(dx)>11||dy===-14||dy>12;
   const shade=edge?mix(body,'#09162b',.55):mix(body,dx<0?'#ffffff':'#101341',dx<0?clamp((-dx-dy)*.016,0,.42):clamp(dx*.038,0,.45));
   r(x+dx,y+dy,1,1,shade);
  }
  if(bodyFraction>.8)r(x-9,y-8,3,2,mix(body,'#ffffff',.7));
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
// Clockwise track distance is the shared coordinate for the chase. The player
// turns at the bottom-center pellet, then at the approaching ghosts at top-center.
// Ghosts leave home at normal speed with staggered departures, so their final
// positions naturally become the first frame of the next chase.
export function motionAt(seconds,variant='B') {
 const t=wrap(seconds,DURATION),cabinet=variant==='C';
 const top=cabinet?112:98,bottom=cabinet?350:408,left=cabinet?125:76,right=cabinet?835:884;
 const py=cabinet?177:183,ph=cabinet?108:143,houseY=py+ph+19;
 const length=2*(right-left+bottom-top),speed=length/20,start=(right-left)/2,home=length/2+start;
 const track=d=>pointOnTrack(d,top,bottom,left,right);
 const powered=t>=10&&t<30,d=t<10?start+t*speed:home-(t-10)*speed;
 const player={...track(d),powered};player.dir=player.dir.map(v=>v*(t<10?1:-1));
 const ghosts=ghostColors.map((color,i)=>{
  const gap=56*(i+1),catchAt=10+gap/(speed*.42),caughtD=home-(catchAt-10)*speed;
  const slotX=480+(i-1.5)*30,eyeSpeed=speed*2,exitAt=30+gap/speed;
  const exitStart=exitAt-(Math.abs(slotX-480)+bottom-houseY)/speed,rebornAt=exitStart-1;
  const data={color,bodyFraction:1,caught:track(caughtD),catchAge:t-catchAt};
  const at=(x,y,dir,mode,extra={})=>({...data,x,y,dir,mode,...extra});
  const along=(distance,direction,mode)=>{const p=track(distance);return at(p.x,p.y,p.dir.map(v=>v*direction),mode);};
  if(t<10)return along(start+t*speed-gap,1,'chase');
  if(t<catchAt)return along(home-gap-(t-10)*speed*.58,-1,'frightened');
  let delta=wrap(home-caughtD,length);if(delta>length/2)delta-=length;
  let age=t-catchAt,travel=Math.abs(delta)/eyeSpeed;
  if(age<travel)return along(caughtD+Math.sign(delta)*age*eyeSpeed,Math.sign(delta),'eyes');
  age-=travel;travel=(bottom-houseY)/eyeSpeed;
  if(age<travel)return at(480,bottom-age*eyeSpeed,[0,-1],'eyes');
  age-=travel;travel=Math.abs(slotX-480)/eyeSpeed;
  if(age<travel)return at(480+Math.sign(slotX-480)*age*eyeSpeed,houseY,[Math.sign(slotX-480),0],'eyes');
  if(t<rebornAt)return at(slotX,houseY,[0,1],'home');
  if(t<exitStart)return at(slotX,houseY,[0,1],'reborn',{bodyFraction:t-rebornAt});
  if(t<exitAt){
   const distance=(t-exitStart)*speed,horizontal=Math.abs(slotX-480);
   if(distance<horizontal)return at(slotX+Math.sign(480-slotX)*distance,houseY,[Math.sign(480-slotX),0],'exit');
   return at(480,houseY+distance-horizontal,[0,1],'exit');
  }
  return along(home+(t-exitAt)*speed,1,'chase');
 });
 return {player,ghosts,d,t,top,bottom,left,right,py,ph,length,speed,start,home,track};
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
 const cabinet=variant==='C',scene=motionAt(t,variant);
 const {top,bottom,py,ph,length,speed,start,home,d,track,player,ghosts}=scene,powered=player.powered;
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
  const pd=i*24,p=track(pd),visits=[];
  const clockwise=wrap(pd-start,length)/speed,counterclockwise=wrap(home-pd,length)/speed;
  if(clockwise<=10)visits.push(clockwise);
  visits.push(10+counterclockwise);if(counterclockwise<=10)visits.push(30+counterclockwise);
  const sinceEaten=Math.min(...visits.map(at=>wrap(t-at,DURATION)));
  if(sinceEaten>6){r(p.x-2,p.y-2,4,4,'#26344b');r(p.x-1,p.y-2,3,3,'#efcc9e');r(p.x-1,p.y-2,1,1,'#fff3d3');}
 }
 if(!powered){const p=track(home);orb(r,p.x,p.y,8,'#243454');orb(r,p.x,p.y,5,Math.floor(t*4)%2?'#fff3be':'#f5b656');r(p.x-2,p.y-3,2,2,'#ffffff');}
 // Text is drawn on the plate's empty glass sign, never baked into an image.
 const clean=text.toUpperCase(),available=cabinet?310:374,size=Math.min(5,Math.max(1,Math.floor(available/Math.max(1,clean.length*6-1))));
 centred(r,clean,py+Math.floor((ph-7*size)/2)-2,size,'#fff0c8');
 centred(r,'STREAM PAUSED',py+ph-20,1,'#588eaa');
 // Native sprite pixels scale as whole rectangles, avoiding blurred character edges.
 const sprite=(x,y)=> (sx,sy,w,h,c)=>r(x+(sx-x)*.8,y+(sy-y)*.8,Math.max(1,w*.8),Math.max(1,h*.8),c);
 const shadow=(x,y)=>{r(x-11,y+12,23,2,'#061329');};
 for(const actor of ghosts){
  const {x,y,color,dir,mode,bodyFraction,catchAge,caught}=actor;
  const eyesOnly=mode==='eyes'||mode==='home';
  if(!eyesOnly)shadow(x,y);
  ghost(sprite(x,y),x,y,color,t,dir,mode==='frightened',eyesOnly,bodyFraction);
  if(catchAge>=0&&catchAge<.75)lettering(r,String(200*2**ghostColors.indexOf(color)),caught.x-10,caught.y-21-catchAge*12,1,'#9eeaff');
 }
 shadow(player.x,player.y);pacman(sprite(player.x,player.y),player.x,player.y,t,player.dir,powered);
 // Moving light packets remain on the existing luminous outer rail.
 for(let i=0;i<3;i++){
  const p=track(d+i*length/3),pulse=powered?'#fff1bb':'#b3f9ff';
  if(Math.abs(p.y-top)<1)r(p.x-4,top-(cabinet?16:20),8,2,pulse);
  if(Math.abs(p.y-bottom)<1)r(p.x-4,bottom+(cabinet?13:16),8,2,pulse);
 }
 // Sparse cabinet lamps brighten without painting rectangular halos over artwork.
 if(cabinet){for(const [x,y] of [[69,220],[891,218],[356,424],[605,424]]){const b=.2+.4*(1+Math.sin(t*Math.PI/2+x));r(x,y,2,6,mix('#935819','#ffe69a',b));}}
}
export function phaseAt(t){t=wrap(t,DURATION);return t<10?'Ghost chase':t<16?'Power pellet / reversal':t<28?'Returning eyes / recharge':t<33?'Ghosts reborn / leaving home':'Meeting the chase / loop return';}
