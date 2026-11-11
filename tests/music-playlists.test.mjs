import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
async function fixture() {
 class Element {
  constructor(tag,text=''){this.tag=tag;this.textContent=text;this.children=[];this.value='';this.classList={add(){}};}
  append(...children){this.children.push(...children);}
  replaceChildren(...children){this.children=children;}
  setAttribute(){}
  focus(){this.focused=true;}
  all(){return [this,...this.children.flatMap(c=>c.all())];}
 }
 const root=new Element('section'),calls=[],listeners={};let failing=false;
 const playlists=[{id:'p',name:'Workshop',revision:1,duration:2,tracks:[{id:'a',revision:1},{id:'b',revision:1}],uses:[]}];
 const assets=['a','b'].map(id=>({id,name:`Track ${id}`,kind:'audio',revision:1,revisions:[{revision:1,duration_seconds:1}]}));
 const window={addEventListener:(type,fn)=>{listeners[type]=fn;},dispatchEvent(){},confirm:()=>true};
 const context={window,document:{getElementById:()=>root,createElement:tag=>new Element(tag)},Option:class extends Element {constructor(label,value){super('option',label);this.value=value;}},Event,AbortSignal,location:{origin:'http://localhost:18871'},fetch:async(url,options)=>{calls.push({path:new URL(url).pathname,...options});return {ok:!failing,text:async()=> 'Playlist changed; reload before editing',json:async()=>new URL(url).pathname==='/api/generator/assets'?assets:playlists};}};
 vm.runInNewContext(readFileSync('internal/relay/web/music-playlists.js','utf8'),context);
 await new Promise(resolve=>setImmediate(resolve));
 const button=text=>root.all().find(e=>e.tag==='button'&&e.textContent===text);
 const name=root.all().find(e=>e.tag==='input'), form=root.all().find(e=>e.tag==='form');
 return {root,calls,button,name,form,refresh:window.musicPlaylists.refresh,fail(){failing=true;},async submit(){await form.onsubmit({preventDefault(){}});},unload(){let blocked=false;listeners.beforeunload({preventDefault(){blocked=true;}});return blocked;}};
}
test('reordering a saved playlist is a dirty edit and saves exact ordered references',async()=>{
 const ui=await fixture();ui.button('Edit playlist').onclick();assert.equal(ui.unload(),false);
 ui.button('Move down').onclick();assert.equal(ui.unload(),true);
 await ui.submit();const save=JSON.parse(ui.calls.find(c=>c.method==='PUT').body);
 assert.deepEqual(save.tracks,[{id:'b',revision:1},{id:'a',revision:1}]);assert.equal(save.version,1);assert.equal(ui.unload(),false);
});
test('stale playlist save preserves name and reordered tracks through refresh',async()=>{
 const ui=await fixture();ui.button('Edit playlist').onclick();ui.name.value='My draft';ui.button('Move down').onclick();await ui.refresh();
 assert.equal(ui.name.value,'My draft');ui.fail();await ui.submit();assert.equal(ui.name.value,'My draft');assert.equal(ui.unload(),true);
 assert.ok(ui.root.all().some(e=>e.textContent.includes('Playlist changed')));
});

test('playlist creation stays out of browsing until requested',async()=>{
 const ui=await fixture();assert.equal(ui.form.hidden,true);ui.button('New playlist').onclick();assert.equal(ui.form.hidden,false);assert.equal(ui.name.focused,true);
 ui.button('Cancel edit').onclick();assert.equal(ui.form.hidden,true);
});
