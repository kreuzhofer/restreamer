import test from 'node:test';
import assert from 'node:assert/strict';
import {drawFrame, sceneAt, WIDTH, HEIGHT, DURATION, FPS} from './animation.mjs';
import {rasterCanvas} from './raster.mjs';

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
