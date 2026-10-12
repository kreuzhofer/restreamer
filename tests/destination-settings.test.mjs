import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

async function settings() {
  class Element {
    constructor(tag) {this.tag=tag;this.children=[];this.dataset={};this.value='';this.textContent='';}
    append(...children){this.children.push(...children);}
    replaceChildren(...children){this.children=children;}
    setAttribute(){}
    focus(){focused=this;}
    all(){return [this,...this.children.flatMap(child=>child.all())];}
    reset(){this.all().filter(child=>child.tag==='input').forEach(child=>{child.value='';});}
  }
  let focused, fail=false, status={version:1,managed:true,available:true,destinations:[{name:'local',url:'rtmp://localhost/app',has_key:true,ready:true}]};
  const container=new Element('section'), listeners={},calls=[];
  const window={addEventListener:(name,fn)=>{listeners[name]=fn;},dispatchEvent(){},confirm:()=>true};
  const context=vm.createContext({window,document:{createElement:tag=>new Element(tag),querySelectorAll:()=>[]},location:{origin:'http://localhost:18850'},AbortSignal,Event,
    fetch:async(url,options)=>{assert.ok(url.startsWith('http://localhost:18850/'));const path=new URL(url).pathname;calls.push({path,...options});return {ok:!fail,text:async()=> 'Cannot save destinations; previous configuration retained',json:async()=>path==='/api/stage'?{stage:'OFF',mode:'off'}:status};}});
  vm.runInContext(readFileSync(new URL('../internal/relay/web/destination-settings.js',import.meta.url),'utf8'),context);
  const mounted=window.mountDestinationSettings(container);
  await new Promise(resolve=>setImmediate(resolve));
  const button=text=>container.all().find(element=>element.tag==='button'&&element.textContent===text);
  const input=label=>container.all().find(element=>element.tag==='label'&&element.textContent===label).children[0];
  const form=container.all().find(element=>element.tag==='form');
  return {mounted,calls,input,button,container,async submit(){await form.onsubmit({preventDefault(){}});await new Promise(resolve=>setImmediate(resolve));},fail(){fail=true;},unload(){let blocked=false;listeners.beforeunload({preventDefault(){blocked=true;}});return blocked;},focused:()=>focused};
}

test('edit keeps the stored key masked and omits unchanged secrets on save',async()=>{
 const ui=await settings();ui.button('Edit').onclick();assert.equal(ui.input('Stream key').value,'');assert.equal(ui.input('Stream key').type,'password');
 ui.input('RTMP or RTMPS server URL').value='rtmp://localhost/updated';await ui.submit();
 const save=ui.calls.find(call=>call.method==='PUT');assert.ok(save);assert.equal(Object.hasOwn(JSON.parse(save.body),'stream_key'),false);
});

test('failed writes preserve the draft and guard navigation; successful refresh does not clear it',async()=>{
 const ui=await settings();ui.button('Edit').onclick();ui.input('Stream key').value='dummy-replacement';
 await ui.mounted.refresh();assert.equal(ui.input('Stream key').value,'dummy-replacement');assert.equal(ui.unload(),true);
 ui.fail();await ui.submit();assert.equal(ui.input('Stream key').value,'dummy-replacement');assert.equal(ui.unload(),true);
 assert.ok(ui.container.all().some(element=>element.textContent.includes('Cannot save destinations')));
});

test('removing another destination preserves an unrelated unsaved credential draft',async()=>{
 const ui=await settings();ui.input('Unique name').value='draft';ui.input('Stream key').value='draft-key';
 await ui.button('Remove').onclick();
 assert.equal(ui.input('Unique name').value,'draft');assert.equal(ui.input('Stream key').value,'draft-key');assert.equal(ui.unload(),true);
});
