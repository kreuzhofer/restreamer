import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
async function fixture({delaySave=false}={}) {
 class Element {
  constructor(tag,text=''){this.tag=tag;this.textContent=text;this.children=[];this.value='';this.classList={add(){}};this.listeners={};}
  append(...children){for(const child of children){child.parent=this;this.children.push(child);}}
  insertBefore(child,before){if(child.parent)child.remove();child.parent=this;const at=before?this.children.indexOf(before):this.children.length;this.children.splice(at,0,child);}
  prepend(child){child.parent=this;this.children.unshift(child);}
  replaceChildren(...children){this.children=[];this.append(...children);}
  replaceWith(child){const at=this.parent.children.indexOf(this);this.parent.children[at]=child;child.parent=this.parent;}
  remove(){this.parent.children=this.parent.children.filter(c=>c!==this);}
  setAttribute(){}
  addEventListener(name,handler){this.listeners[name]=handler;}
  reportValidity(){return true;}
  focus(){}
  closest(){return null;}
  all(){return [this,...this.children.flatMap(c=>c.all())];}
 }
 const root=new Element('section'),timers=[],calls=[];
 let resolveSave,saveCount=0;const saveWait=delaySave?new Promise(resolve=>{resolveSave=resolve;}):Promise.resolve();
 let jobs=[{id:'ready',kind:'brb',state:'ready',sequence:1,duration:30,media_revision:'revision',created_at:'2026-10-28T10:00:00Z',design_snapshot:{id:'saved-brb',version:1,name:'Workshop',duration_mode:'music'}}];
 const window={addEventListener(){},confirm:()=>true};
 const context={window,document:{getElementById:()=>root,createElement:tag=>new Element(tag)},Option:class extends Element {constructor(label,value){super('option',label);this.value=value;}},URL,URLSearchParams,AbortSignal,setTimeout,clearTimeout,history:{replaceState(){}},location:{origin:'http://localhost:18871'},setInterval:fn=>timers.push(fn),fetch:async(url,options={})=>{const path=new URL(url).pathname;calls.push({path,...options});if(path==='/api/generator/designs'&&options.method==='POST'||path.startsWith('/api/generator/designs/')&&options.method==='PUT'){if(++saveCount===1)await saveWait;return {ok:true,json:async()=>({...JSON.parse(options.body),id:'saved-brb',version:saveCount})};}return {ok:true,json:async()=>path==='/api/generator/jobs'?(options.method==='POST'?jobs[0]:jobs):[]};}};
 vm.createContext(context);
 for(const name of ['music-selection','media-presentation','brb-presentations']) vm.runInContext(readFileSync(`internal/relay/web/${name}.js`,'utf8'),context);
 const flush=()=>new Promise(resolve=>setImmediate(resolve));await flush();
 return {root,calls,resolveSave,flush,input:label=>root.all().find(e=>e.tag==='label'&&e.textContent===label).children[0],form:root.all().find(e=>e.tag==='form'),save:root.all().find(e=>e.tag==='button'&&e.textContent==='Save changes'),video:()=>root.all().find(e=>e.tag==='video'),async poll(){timers[0]();await flush();},queue(){jobs=[...jobs,{id:'new',kind:'brb',state:'running',sequence:2,duration:40,progress:20,design_snapshot:{name:'Next',duration_mode:'music'}}];}};
}
test('status polling preserves the exact video element being reviewed',async()=>{
 const ui=await fixture(),video=ui.video();video.currentTime=12;video.paused=false;
 await ui.poll();assert.equal(ui.video(),video);assert.equal(ui.video().currentTime,12);
 ui.queue();await ui.poll();assert.equal(ui.video(),video);assert.equal(ui.video().currentTime,12);
 assert.ok(ui.root.all().some(e=>e.tag==='progress'));
});


test('generation waits for an in-flight save and captures newer BRB edits',async()=>{
 const ui=await fixture({delaySave:true});
 ui.input('Message').value='FIRST';ui.form.listeners.input();
 const saving=ui.save.onclick();await ui.flush();
 ui.input('Message').value='LATEST';ui.form.listeners.input();
 const generating=ui.form.onsubmit({preventDefault(){}});
 ui.resolveSave();await saving;await generating;
 const saves=ui.calls.filter(c=>c.path.startsWith('/api/generator/designs')&&c.method!=='GET');
 assert.equal(saves.length,2);assert.equal(JSON.parse(saves[1].body).scenes[0].text,'LATEST');
 const job=ui.calls.find(c=>c.path==='/api/generator/jobs'&&c.method==='POST');
 assert.equal(JSON.parse(job.body).version,2);assert.equal(JSON.parse(job.body).design_id,'saved-brb');
});

test('a video name draft survives polling and can be cancelled',async()=>{
 const ui=await fixture();
 ui.root.all().find(e=>e.tag==='button'&&e.textContent==='Name this video').onclick();
 const input=ui.input('Video name');input.value='My quieter break';
 await ui.poll();assert.equal(ui.input('Video name'),input);assert.equal(input.value,'My quieter break');
 const cancel=ui.root.all().find(e=>e.tag==='button'&&e.textContent==='Cancel name');
 assert.ok(cancel,'renaming needs an explicit cancel action');cancel.onclick();
 assert.equal(ui.root.all().filter(e=>e.tag==='label'&&e.textContent==='Video name').length,0);
});

 test('BRB results identify captured inputs and older snapshots after a later autosave',async()=>{
 const ui=await fixture();await ui.save.onclick();await ui.form.onsubmit({preventDefault(){}});
 assert.ok(ui.root.all().some(e=>e.textContent.includes('Matches current design')));
 assert.ok(ui.root.all().some(e=>e.textContent.includes('No music')));
 assert.ok(ui.root.all().some(e=>e.textContent.includes('2026')));
 const reviewed=ui.video();ui.input('Message').value='NEWER DRAFT';ui.form.listeners.input();await ui.save.onclick();
 assert.ok(ui.root.all().some(e=>e.textContent.includes('Based on earlier changes')));
 assert.equal(ui.video(),reviewed,'draft status updates must not interrupt exact preview');
 assert.ok(ui.root.all().some(e=>e.textContent.includes('First retained render')));
 });
