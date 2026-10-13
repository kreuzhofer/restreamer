import {WIDTH,HEIGHT} from './animation.mjs';
export function raster(){
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
