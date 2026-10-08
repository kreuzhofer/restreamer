// Integer fillRect adapter for the Canvas drawing source. No browser, native
// modules or fonts are needed to render the same pixel artwork offline.
export function rasterCanvas(width, height) {
  const pixels = Buffer.alloc(width*height*3);
  const colors = new Map();
  return {pixels, fillStyle:'#000000', fillRect(x,y,w,h) {
    if (!colors.has(this.fillStyle)) {
      if (!/^#[0-9a-f]{6}$/i.test(this.fillStyle)) throw new Error('Expected RGB hex colour');
      colors.set(this.fillStyle, [1,3,5].map(i=>parseInt(this.fillStyle.slice(i,i+2),16)));
    }
    const color=colors.get(this.fillStyle);
    for(let py=Math.max(0,y);py<Math.min(height,y+h);py++) {
      for(let px=Math.max(0,x);px<Math.min(width,x+w);px++) pixels.set(color,(py*width+px)*3);
    }
  }};
}
