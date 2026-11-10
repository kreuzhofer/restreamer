import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
function fixture() {
  const nodes = new Map();
  const el = id => { if (!nodes.has(id)) nodes.set(id, {textContent:'',handlers:{},setAttribute(){},addEventListener(name,fn){this.handlers[name]=fn;},replaceChildren(){},showModal(){},focus(){}}); return nodes.get(id); };
  const previewCalls = [];
  const window = {broadcastPreview:{setPath:(...args)=>previewCalls.push(['path',...args]),update:(...args)=>previewCalls.push(['update',...args])}};
  const context = {window, document:{getElementById:el,dispatchEvent(){}}, sessionStorage:{getItem:()=>null}, Event:class {}};
  vm.createContext(context);
  vm.runInContext(readFileSync('internal/relay/web/stages.js','utf8'),context);
  const stage = {stage:'PRESTREAM',source:'file',mode:'preview_only',server_id:'test',context:'test:1',media:{},playback:{}};
  const outputs = [];
  const update = (online=true) => window.broadcastStages.update({stage,outputs},online);
  update();
  return {el,stage,outputs,previewCalls,update,api:window.broadcastStages};
}
test('ending action explains the selected ending completion behavior', () => {
  const ui = fixture();
  ui.api.setSelections({ending:'loop'},[{id:'loop',presentation:{mode:'music'}}]);
  assert.equal(ui.el('stage-ending-help').textContent,'Loop ending · stop manually');
  ui.api.setSelections({ending:'finite'},[{id:'finite'}]);
  assert.equal(ui.el('stage-ending-help').textContent,'Play ending, then stop');
});
test('suspended looping position remains inside its displayed cycle', () => {
  const ui = fixture();
  ui.stage.return_stage='PRESTREAM';
  ui.stage.return_playback={state:'suspended',position:29,duration:2,loop:true};
  ui.update();
  assert.match(ui.el('stage-return').textContent,/0:01 \/ 0:02/);
});
test('active ending hint keeps its pinned behavior when the next assignment changes', () => {
  const ui = fixture();
  ui.stage.stage='ENDING'; ui.stage.playback.loop=true; ui.update();
  ui.api.setSelections({ending:'finite'},[{id:'finite'}]);
  assert.equal(ui.el('stage-ending-help').textContent,'Loop ending · stop manually');
  ui.stage.playback.loop=false; ui.update();
  ui.api.setSelections({ending:'loop'},[{id:'loop',presentation:{mode:'music'}}]);
  assert.equal(ui.el('stage-ending-help').textContent,'Play ending, then stop');
});

test('stopped session offers explicit rehearsal or broadcast with the chosen starting source', () => {
  const ui=fixture();ui.stage.stage='OFF';ui.stage.mode='real';ui.update();
  assert.equal(ui.el('stage-start').hidden,false);
  assert.equal(ui.el('stage-switches').hidden,true);
  ui.el('stage-start-source').value='go_live';ui.el('stage-start-rehearsal').handlers.click();
  assert.match(ui.el('stage-dialog-effect').textContent,/Preview only: no destination/);
  ui.el('stage-dialog').handlers.close();
  ui.el('stage-start-broadcast').handlers.click();
  assert.match(ui.el('stage-dialog-effect').textContent,/Selected destinations will receive/);
  ui.stage.stage='PRESTREAM';ui.update();assert.equal(ui.el('stage-start').hidden,true);assert.equal(ui.el('stage-switches').hidden,false);
});


test('continuous monitor describes actual fallback and pending OBS with unknown delay',()=>{
 const ui=fixture();ui.outputs.push({output_mode:'continuous'});ui.stage.stage='LIVE';ui.stage.source='obs';
 ui.stage.continuous={state:'running',source:'brb',requested_source:'obs',selection_pending:true,estimated_delay_ms:null,epoch:4};ui.update();
 assert.equal(ui.el('broadcast-path-label').hidden,true);
 assert.equal(ui.el('stage-source').textContent,'Fallback BRB');
 assert.match(ui.el('broadcast-path-status').textContent,/Fallback BRB.*waiting for OBS.*delay unknown/);
 assert.deepEqual(ui.previewCalls.at(-1),['update',true,4]);
 ui.stage.continuous.state='recovering';ui.stage.continuous.source='unavailable';ui.update();
 assert.match(ui.el('broadcast-path-status').textContent,/Recovering encoder/);
 assert.deepEqual(ui.previewCalls.at(-1),['update',false,4]);
 ui.update(false);assert.equal(ui.el('broadcast-path-status').textContent,'Output status unavailable');
});

test('mixed-path monitor selection follows its own epoch without changing stage intent',()=>{
 const ui=fixture();ui.outputs.push({output_mode:'continuous'},{output_mode:'passthrough'});
 ui.stage.continuous={state:'running',source:'file',estimated_delay_ms:null,epoch:9};ui.stage.playback.epoch=3;ui.update();
 assert.equal(ui.el('broadcast-path-label').hidden,false);
 assert.deepEqual(ui.previewCalls.at(-1),['update',true,3]);
 ui.el('broadcast-path').handlers.change({target:{value:'continuous'}});
 assert.deepEqual(ui.previewCalls.at(-1),['update',true,9]);
 assert.equal(ui.stage.stage,'PRESTREAM');
});
