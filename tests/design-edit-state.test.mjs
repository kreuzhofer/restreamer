import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
const window={};vm.runInNewContext(readFileSync('internal/relay/web/design-edit-state.js','utf8'),{window});
const ui=window.designEditState;
const draft=()=>({id:'design',version:1,name:'Welcome',stage:'prestream',theme:{id:'arcade-after-hours',revision:2},scenes:[{id:'one',layout:'title',text:'WELCOME',duration_seconds:10}]});
test('no-op and reverted edits compare equal despite form defaults and server metadata',()=>{
 const saved=draft(), edited=structuredClone(saved);edited.version=2;edited.updated_at='later';edited.duration_mode='fixed';edited.loop_transition={kind:'cut'};
 Object.assign(edited.scenes[0],{font:'',font_size:0,alignment:'',media_kind:'image',content_region:{width_percent:0,height_percent:0},transition:{kind:'cut'}});
 assert.equal(ui.contentKey(saved),ui.contentKey(edited));
 edited.scenes[0].text='DIFFERENT';assert.notEqual(ui.contentKey(saved),ui.contentKey(edited));
 edited.scenes[0].text='WELCOME';assert.equal(ui.contentKey(saved),ui.contentKey(edited));
});
test('animation duration derived from loops does not make a saved design dirty',()=>{
 const a=draft();a.scenes[0].animation_loops=2;const b=structuredClone(a);b.scenes[0].duration_seconds=32.52;
 assert.equal(ui.contentKey(a),ui.contentKey(b));b.scenes[0].animation_loops=3;assert.notEqual(ui.contentKey(a),ui.contentKey(b));
});
test('music choices, zero volume and timing changes remain actual edits',()=>{
 const a=draft();a.soundtrack={asset:{id:'track',revision:1},mode:'end'};const b=structuredClone(a);b.soundtrack.volume_percent=100;b.soundtrack.fade_in_seconds=0;
 assert.equal(ui.contentKey(a),ui.contentKey(b));b.soundtrack.volume_percent=0;assert.notEqual(ui.contentKey(a),ui.contentKey(b));
});
