import test from 'node:test';
import assert from 'node:assert/strict';
import {DURATION,motionAt} from './animation.mjs';

// Follow visible actors through every phase, including the video seam. Matching
// the first frame alone cannot detect a teleport just before that frame.
test('actors travel continuously through captures, home, exits and the loop seam',()=>{
 for(const variant of ['maze']){
  let previous=motionAt(-1/120);
  for(let frame=0;frame<=DURATION*120;frame++){
   const current=motionAt(frame/120);
   for(const [a,b] of [[previous.player,current.player],...previous.ghosts.map((g,i)=>[g,current.ghosts[i]])]){
    assert.ok(Math.hypot(a.x-b.x,a.y-b.y)<3,`${variant}: actor jumped at ${frame/120}s`);
   }
   previous=current;
  }
 }
});

test('each ghost regrows at home and exits bottom-center clockwise at chase speed',()=>{
 for(const variant of ['maze']){
  const exits=[];
  for(let i=0;i<4;i++){
   let previous=motionAt(20).ghosts[i],sawRebirth=false,sawExit=false;
   for(let frame=20*120;frame<40*120;frame++){
    const t=frame/120,scene=motionAt(t),g=scene.ghosts[i];
    if(g.mode==='reborn'){
     sawRebirth=true;
     assert.equal(g.y,scene.py+scene.ph+19);
     assert.ok(g.bodyFraction>=0&&g.bodyFraction<=1);
    }
    if(g.mode==='exit')sawExit=true;
    if(g.mode==='chase'&&previous.mode==='exit'){
     assert.ok(sawRebirth&&sawExit,'ghost must regrow and walk out before chasing');
     assert.ok(t>30,'Pac-Man must complete his lap before ghosts emerge');
     assert.ok(Math.abs(g.x-480)<2&&g.y===scene.bottom,'exit must meet the bottom-center corridor');
     assert.deepEqual(g.dir,[-1,0],'right turn while leaving downward means clockwise/left');
     const later=motionAt(t+.1).ghosts[i];
     assert.ok(Math.abs(Math.hypot(later.x-g.x,later.y-g.y)-scene.speed*.1)<.001,'no catch-up speed boost');
     exits.push(t);break;
    }
    previous=g;
   }
  }
  assert.equal(exits.length,4);
  assert.ok(exits.every((t,i)=>i===0||t>exits[i-1]+.4),'departures should be visibly staggered');
 }
});

test('loop closes with ghosts approaching and Pac-Man turning away without jumping',()=>{
 const last=motionAt(DURATION-.01),first=motionAt(0),next=motionAt(.01);
 assert.equal(last.player.dir[0],-1);
 assert.ok(last.player.dir[1]===0);
 assert.deepEqual(first.player.dir,[1,0]);
 assert.ok(next.player.x>first.player.x);
 for(let i=0;i<4;i++){
  assert.equal(last.ghosts[i].mode,'chase');
  assert.deepEqual(last.ghosts[i].dir,[1,0]);
  assert.ok(first.ghosts[i].x<first.player.x);
  assert.ok(Math.hypot(last.ghosts[i].x-first.ghosts[i].x,last.ghosts[i].y-first.ghosts[i].y)<2);
 }
});
