// Editable, deterministic Canvas animation. Seconds are the only clock: preview
// and offline rendering use this exact source. All drawing uses integer pixels.
export const WIDTH = 320, HEIGHT = 180, FPS = 60, DURATION = 32;
export const palette = {background:'#080e20', wall:'#243976', edge:'#4776cc', text:'#dcf1ea', yellow:'#ffdb46', ghosts:['#fb607f','#f6aacb','#5cdbdb','#ffa95b'], frightened:'#526aff'};

export function sceneAt(seconds) {
  const t = ((seconds % DURATION) + DURATION) % DURATION;
  const index = Math.floor(t / 8);
  return {index, time:t % 8, mode:['chase','powered','scatter','chase'][index], direction:index === 1 || index === 3 ? -1 : 1};
}

const letters = {
  B:['11110','10001','10001','11110','10001','10001','11110'],
  E:['11111','10000','10000','11110','10000','10000','11111'],
  R:['11110','10001','10001','11110','10100','10010','10001'],
  I:['11111','00100','00100','00100','00100','00100','11111'],
  G:['01111','10000','10000','10111','10001','10001','01111'],
  H:['10001','10001','10001','11111','10001','10001','10001'],
  T:['11111','00100','00100','00100','00100','00100','00100'],
  A:['01110','10001','10001','11111','10001','10001','10001'],
  C:['01111','10000','10000','10000','10000','10000','01111'],
  K:['10001','10010','10100','11000','10100','10010','10001'],
};

export function drawFrame(ctx, seconds) {
  const scene = sceneAt(seconds), t = scene.time;
  const rect = (x,y,w,h,c) => {ctx.fillStyle=c; ctx.fillRect(Math.round(x),Math.round(y),w,h);};
  rect(0,0,WIDTH,HEIGHT,palette.background);
  // A quiet arcade cabinet border and two lanes leave the message unobscured.
  for (const y of [24,64,114,156]) {
    rect(12,y,296,1,palette.edge); rect(12,y+2,296,1,palette.wall);
    for (const x of [12,306]) rect(x,y,2,7,palette.wall);
  }
  for (const x of [24,88,152,216,280]) {
    rect(x,12,16,2,palette.wall); rect(x+8,169,8,1,palette.edge);
  }
  const text = 'BE RIGHT BACK', size = 3, left = Math.floor((WIDTH-(text.length*6-1)*size)/2);
  for (let i=0;i<text.length;i++) {
    (letters[text[i]] || []).forEach((row,y) => [...row].forEach((pixel,x) => {
      if (pixel === '1') {
        rect(left+i*18+x*size+1,80+y*size+1,3,3,'#172a40');
        rect(left+i*18+x*size,79+y*size,3,3,palette.text);
      }
    }));
  }
  // Pellets disappear behind the approaching player; they reset off-screen.
  const topX = scene.index === 0 ? -30+46*t : 338-55*t;
  for (let x=20;x<305;x+=16) {
    if (scene.index !== 0 || x > topX+8) rect(x,45,2,2,'#b9abb5');
    rect(x,135,2,2,'#b9abb5');
  }
  if (scene.index === 0 && topX < 292) rect(296,42,6,6,Math.floor(t*4)%2 ? palette.text : palette.yellow);

  function eyes(x,y,direction) {
    for (const dx of [-4,3]) {
      rect(x+dx-1,y-3,5,6,'#f5f6ff');
      rect(x+dx+(direction>0?2:0),y-1,2,3,'#243976');
    }
  }
  function ghost(x,y,color,frightened=false,onlyEyes=false) {
    if (!onlyEyes) {
      const rows = ['00001111110000','00111111111100','01111111111110','01111111111110','11111111111111','11111111111111','11111111111111','11111111111111','11111111111111','11111111111111','11111111111111','11111111111111',Math.floor(t*6)%2?'11001100110011':'11100111001110'];
      rows.forEach((row,dy) => [...row].forEach((p,dx) => {if(p==='1')rect(x-7+dx,y-6+dy,1,1,frightened?palette.frightened:color);}));
    }
    if (frightened && !onlyEyes) {
      rect(x-4,y-2,2,2,palette.text); rect(x+2,y-2,2,2,palette.text);
      for(let i=0;i<7;i++) rect(x-3+i,y+3+i%2,1,1,palette.text);
    } else eyes(x,y,onlyEyes ? -1 : scene.direction);
  }
  function pacman(x,y,direction) {
    const mouth = [0.12,0.5,0.9,0.5][Math.floor(t*10)%4];
    for (let dy=-7;dy<=7;dy++) for(let dx=-7;dx<=7;dx++) {
      if (dx*dx+dy*dy<=53 && !(dx*direction>=0 && Math.abs(dy)<=dx*direction*mouth)) rect(x+dx,y+dy,1,1,palette.yellow);
    }
    rect(x+(direction>0?0:-1),y-4,2,2,palette.background);
  }
  if (scene.index < 2) {
    pacman(topX,46,scene.direction);
    palette.ghosts.forEach((color,i) => ghost(topX-24*(i+1),46,color,scene.mode==='powered' || t>7.17));
  } else if (scene.mode === 'scatter') {
    const x=-170+84*t;
    pacman(x,136,1);
    palette.ghosts.forEach((color,i) => ghost(x+30*(i+1),130+(i%2)*12,color,true));
    // Returning eyes take the empty upper lane back toward the ghost house.
    for(let i=0;i<2;i++) ghost(390-65*t+i*28,46,palette.ghosts[i],false,true);
  } else {
    const x=450-76*t;
    pacman(x,136,-1);
    palette.ghosts.forEach((color,i) => ghost(x+24*(i+1),136,color));
  }
}
