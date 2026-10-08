import test from 'node:test';
import assert from 'node:assert/strict';
import {drawFrame, sceneAt, WIDTH, HEIGHT, DURATION, FPS} from './animation.mjs';
import {rasterCanvas} from './raster.mjs';
import letters from './font.json' with {type:'json'};

test('font covers supported text and custom messages remain on one auto-sized line', () => {
  for(const ch of "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ÄÖÜß .,!?:'-/()+&") {
    assert.equal(letters[ch]?.length,7,`missing glyph ${ch}`);
    for(const row of letters[ch]) assert.match(row,/^[01]{5}$/);
  }
  for(const [length,height] of [[1,21],[16,21],[17,14],[24,14],[25,7],[40,7]]) {
    const canvas=rasterCanvas(WIDTH,HEIGHT);
    drawFrame(canvas,3,'W'.repeat(length));
    let minY=HEIGHT,maxY=-1;
    for(let y=70;y<114;y++)for(let x=0;x<WIDTH;x++) {
      const i=(y*WIDTH+x)*3;
      if(canvas.pixels[i]===220&&canvas.pixels[i+1]===241&&canvas.pixels[i+2]===234) {minY=Math.min(minY,y);maxY=Math.max(maxY,y);}
    }
    assert.equal(maxY-minY+1,height);
  }
});

test('loop closes exactly and preserves the message throughout every scene', () => {
  const frame = t => { const canvas = rasterCanvas(WIDTH, HEIGHT); drawFrame(canvas, t); return canvas.pixels; };
  assert.deepEqual(frame(0), frame(DURATION));
  const first = frame(0);
  for (const t of [2, 7.9, 10, 15.9, 19, 23.9, 27, 31.9]) {
    const pixels = frame(t);
    assert.notDeepEqual(pixels, first);
    for (let y = 76; y < 102; y++) {
      assert.deepEqual(pixels.subarray(y * WIDTH * 3, (y + 1) * WIDTH * 3), first.subarray(y * WIDTH * 3, (y + 1) * WIDTH * 3));
    }
  }
  assert.equal(FPS, 60);
  assert.equal(DURATION, 32);
});

test('both chase directions, frightened ghosts, scatter and returning eyes are explicit scenes', () => {
  assert.equal(sceneAt(3).mode, 'chase');
  assert.equal(sceneAt(11).mode, 'powered');
  assert.equal(sceneAt(19).mode, 'scatter');
  assert.equal(sceneAt(27).mode, 'chase');
  assert.equal(sceneAt(3).direction, 1);
  assert.equal(sceneAt(11).direction, -1);
  assert.equal(sceneAt(27).direction, -1);
});
