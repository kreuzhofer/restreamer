import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
function fixture() {
  const nodes = new Map();
  const el = id => { if (!nodes.has(id)) nodes.set(id, {textContent:'',setAttribute(){},addEventListener(){},replaceChildren(){},showModal(){},focus(){}}); return nodes.get(id); };
  const window = {};
  const context = {window, document:{getElementById:el,dispatchEvent(){}}, sessionStorage:{getItem:()=>null}, Event:class {}};
  vm.createContext(context);
  vm.runInContext(readFileSync('internal/relay/web/stages.js','utf8'),context);
  const stage = {stage:'PRESTREAM',source:'file',mode:'preview_only',server_id:'test',context:'test:1',media:{},playback:{}};
  const update = () => window.broadcastStages.update({stage,outputs:[]},true);
  update();
  return {el,stage,update,api:window.broadcastStages};
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
