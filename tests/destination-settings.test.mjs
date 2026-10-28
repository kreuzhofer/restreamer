import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

async function settings({deferIngests = false, destinationURL = "rtmp://localhost/app"} = {}) {
  class Element {
    constructor(tag) {this.tag=tag;this.children=[];this.dataset={};this.value='';this.textContent='';}
    append(...children){this.children.push(...children);}
    replaceChildren(...children){this.children=children;}
    setAttribute(){}
    focus(){focused=this;}
    all(){return [this,...this.children.flatMap(child=>child.all())];}
    reset(){this.all().filter(child=>child.tag==='input').forEach(child=>{child.value='';});}
  }
  let focused, fail=false, status={version:1,managed:true,available:true,destinations:[{name:'local',url:destinationURL,has_key:true,ready:true}]};
  let resolveIngests; const ingestWait = deferIngests ? new Promise(resolve => {resolveIngests = resolve;}) : Promise.resolve();
  const container=new Element('section'), listeners={},calls=[];
  const window={addEventListener:(name,fn)=>{listeners[name]=fn;},dispatchEvent(){},confirm:()=>true};
  const context=vm.createContext({window,document:{createElement:tag=>new Element(tag),querySelectorAll:()=>[]},location:{origin:'http://localhost:18850'},AbortSignal,Event,
    fetch:async(url,options)=>{assert.ok(url.startsWith('http://localhost:18850/'));const path=new URL(url).pathname;calls.push({path,...options});if(path==='/api/destination-presets/twitch')await ingestWait;return {ok:!fail,text:async()=> 'Cannot save destinations; previous configuration retained',json:async()=>path==='/api/destination-presets/twitch'?[{name:'Frankfurt',url:'rtmps://euc10.contribute.live-video.net/app'}]:path==='/api/stage'?{stage:'OFF',mode:'off'}:status};}});
  vm.runInContext(readFileSync(new URL('../internal/relay/web/destination-settings.js',import.meta.url),'utf8'),context);
  const mounted=window.mountDestinationSettings(container);
  await new Promise(resolve=>setImmediate(resolve));
  const button=text=>container.all().find(element=>element.tag==='button'&&element.textContent===text);
  const input=label=>container.all().find(element=>element.tag==='label'&&element.textContent===label).children[0];
  const form=container.all().find(element=>element.tag==='form');
  return {mounted,calls,input,button,container,resolveIngests,async submit(){await form.onsubmit({preventDefault(){}});await new Promise(resolve=>setImmediate(resolve));},fail(){fail=true;},unload(){let blocked=false;listeners.beforeunload({preventDefault(){blocked=true;}});return blocked;},focused:()=>focused};
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

test('platform selection prefills editable names and secure URLs without saving',async()=>{
 const ui=await settings();const platform=ui.input('Platform');
 assert.deepEqual(platform.children.map(option=>option.textContent),['Custom','Twitch','YouTube','Kick']);
 for(const [value,name,url] of [
   ['twitch','Twitch','rtmps://euc10.contribute.live-video.net/app'],
   ['youtube','YouTube','rtmps://a.rtmps.youtube.com:443/live2'],
 ]) {
   platform.value=value;await platform.onchange();
   assert.equal(ui.input('Unique name').value,name);
   assert.equal(ui.input('RTMP or RTMPS server URL').value,url);
   assert.equal(ui.calls.some(call=>call.method!=='GET'),false);
 }
 ui.input('Unique name').value='YouTube-backup';
 ui.input('RTMP or RTMPS server URL').value='rtmps://b.rtmps.youtube.com:443/live2';
 ui.input('Stream key').value='dummy-key';await ui.submit();
 const saved=JSON.parse(ui.calls.find(call=>call.method==='POST').body);
 assert.equal(saved.name,'YouTube-backup');
 assert.equal(saved.url,'rtmps://b.rtmps.youtube.com:443/live2');
 assert.equal(Object.hasOwn(saved,'platform'),false);
});

test('Kick requires the dashboard URL and switching platforms clears an unrelated key',async()=>{
 const ui=await settings();const platform=ui.input('Platform');
 platform.value='youtube';platform.onchange();ui.input('Stream key').value='youtube-dummy-key';
 platform.value='kick';platform.onchange();
 assert.equal(ui.input('Unique name').value,'Kick');
 assert.equal(ui.input('RTMP or RTMPS server URL').value,'');
 assert.equal(ui.input('Stream key').value,'');
 assert.ok(ui.container.all().some(element=>element.textContent.includes('Kick Creator Dashboard')));
 ui.input('RTMP or RTMPS server URL').value='rtmps://example.invalid:443/app';
 ui.input('Stream key').value='kick-dummy-key';
 platform.value='custom';platform.onchange();
 assert.equal(ui.input('RTMP or RTMPS server URL').value,'rtmps://example.invalid:443/app');
 assert.equal(ui.input('Stream key').value,'kick-dummy-key');
});

test('presets only apply to new destinations and reset after saving or cancelling edits',async()=>{
 const ui=await settings();const platform=ui.input('Platform');
 platform.value='kick';platform.onchange();
 ui.fail();await ui.submit();
 assert.equal(platform.value,'kick','Failed saves retain the selected preset');
 const editUI=await settings();const editPlatform=editUI.input('Platform');
 editUI.button('Edit').onclick();
 assert.equal(editPlatform.disabled,true);
 assert.equal(editUI.input('Unique name').value,'local');
 assert.equal(editUI.input('RTMP or RTMPS server URL').value,'rtmp://localhost/app');
 editUI.button('Cancel edit').onclick();assert.equal(editPlatform.disabled,false);
 editPlatform.value='twitch';await editPlatform.onchange();await editUI.submit();
 assert.equal(editPlatform.value,'custom');assert.equal(editUI.input('Unique name').value,'');
 assert.equal(editUI.container.all().find(element=>element.id==='destination-platform-hint').textContent,'');
});

test('Twitch discovery failure stays explicit and never substitutes a guessed URL',async()=>{
 const ui=await settings();ui.fail();const platform=ui.input('Platform');platform.value='twitch';await platform.onchange();
 assert.equal(ui.input('RTMP or RTMPS server URL').value,'');assert.equal(ui.input('Twitch server').disabled,true);
});


test('late Twitch discovery cannot overwrite another platform or a manually edited URL',async()=>{
 const ui=await settings({deferIngests:true});const platform=ui.input('Platform');
 platform.value='twitch';const pending=platform.onchange();
 platform.value='youtube';await platform.onchange();ui.resolveIngests();await pending;
 assert.equal(ui.input('RTMP or RTMPS server URL').value,'rtmps://a.rtmps.youtube.com:443/live2');
 const edited=await settings({deferIngests:true});const selection=edited.input('Platform');
 selection.value='twitch';const waiting=selection.onchange();edited.input('RTMP or RTMPS server URL').value='rtmps://custom.example/app';edited.resolveIngests();await waiting;
 assert.equal(edited.input('RTMP or RTMPS server URL').value,'rtmps://custom.example/app');
});

test('existing Twitch destinations can discover servers without changing their stored URL or key',async()=>{
 const ui=await settings({destinationURL:'rtmps://ingest.global-contribute.live-video.net:443/app'});
 ui.button('Edit').onclick();await ui.button('Choose Twitch server').onclick();
 assert.equal(ui.input('RTMP or RTMPS server URL').value,'rtmps://ingest.global-contribute.live-video.net:443/app');
 assert.equal(ui.input('Stream key').value,'');
 const server=ui.input('Twitch server');server.value='rtmps://euc10.contribute.live-video.net/app';server.onchange();
 await ui.submit();const saved=JSON.parse(ui.calls.find(call=>call.method==='PUT').body);
 assert.equal(saved.url,'rtmps://euc10.contribute.live-video.net/app');assert.equal(Object.hasOwn(saved,'stream_key'),false);
});


test('destination name constraint accepts spaces and matches its visible help',async()=>{
 const ui=await settings(),name=ui.input('Unique name');
 const pattern=new RegExp(`^(?:${name.pattern})$`,'v');
 for(const value of ['Twitch Main','YouTube Backup','a  b','_-','a'.repeat(64)])assert.ok(pattern.test(value),value);
 for(const value of ['', ' ', ' Twitch','Twitch ', 'Twitch\tMain','Twitch/Main','a'.repeat(65)])assert.equal(pattern.test(value),false,value);
 assert.ok(ui.container.all().some(element=>element.textContent.includes('Spaces are allowed')));
 name.value='Twitch Main';ui.input('RTMP or RTMPS server URL').value='rtmp://localhost/app';await ui.submit();
 assert.equal(JSON.parse(ui.calls.find(call=>call.method==='POST').body).name,'Twitch Main');
});
