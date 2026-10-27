import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
const context = {window:{}};
vm.runInNewContext(readFileSync('internal/relay/web/music-selection.js','utf8'),context);
const music=context.window.musicSelection;
test('keeps a pinned older playlist visible without adopting the edited order',()=>{
 const choices=music.choices([], [{id:'p',revision:2,name:'Workshop',duration:120}], {playlist:{id:'p',revision:1}});
 assert.equal(choices.find(x=>x.value==='playlist:p:1').label,'Workshop · saved version 1');
 assert.equal(music.value({playlist:{id:'p',revision:1}}),'playlist:p:1');
 assert.equal(music.decode('playlist:p:2').playlist.revision,2);
});
test('single-track selections preserve exact uploaded revisions and no-music is explicit',()=>{
 const choices=music.choices([{id:'a',name:'Track',kind:'audio',revisions:[{revision:1,duration_seconds:30},{revision:2,duration_seconds:40}]}],[],{asset:{id:'a',revision:1}});
 assert.equal(choices.length,3);
 assert.equal(music.value({asset:{id:'a',revision:1}}),'a:1');
 assert.equal(music.decode('a:2').asset.revision,2);
 assert.equal(music.decode(''),null);
});
