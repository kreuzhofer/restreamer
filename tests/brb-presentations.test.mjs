import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
async function fixture({delaySave=false,failSave=false,delayJob=false}={}) {
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
 let resolveSave,resolveJob,saveCount=0;const jobWait=delayJob?new Promise(resolve=>{resolveJob=resolve;}):Promise.resolve();const saveWait=delaySave?new Promise(resolve=>{resolveSave=resolve;}):Promise.resolve();
 let jobs=[{id:'ready',kind:'brb',state:'ready',sequence:1,duration:30,media_revision:'revision',created_at:'2026-10-28T10:00:00Z',design_snapshot:{id:'saved-brb',version:1,name:'Workshop',duration_mode:'music'}}];
 const window={addEventListener(){},confirm:()=>true};
 const context={window,document:{getElementById:()=>root,createElement:tag=>new Element(tag)},Option:class extends Element {constructor(label,value){super('option',label);this.value=value;}},URL,URLSearchParams,AbortSignal,setTimeout,clearTimeout,history:{replaceState(){}},location:{origin:'http://localhost:18871'},setInterval:fn=>timers.push(fn),fetch:async(url,options={})=>{const path=new URL(url).pathname;calls.push({path,...options});if(path==='/api/generator/designs'&&options.method==='POST'||path.startsWith('/api/generator/designs/')&&options.method==='PUT'){if(++saveCount===1){await saveWait;if(failSave)return {ok:false,text:async()=>'Temporary save failure'};}return {ok:true,json:async()=>({...JSON.parse(options.body),id:'saved-brb',version:saveCount})};}if(path==='/api/generator/jobs'&&options.method==='POST')await jobWait;return {ok:true,json:async()=>path==='/api/generator/jobs'?(options.method==='POST'?jobs[0]:jobs):[]};}};
 vm.createContext(context);
 for(const name of ['design-edit-state','music-selection','media-presentation','brb-presentations']) vm.runInContext(readFileSync(`internal/relay/web/${name}.js`,'utf8'),context);
 const flush=()=>new Promise(resolve=>setImmediate(resolve));await flush();
 return {root,calls,resolveSave,resolveJob,flush,generate:root.all().find(e=>e.tag==='button'&&e.textContent==='Generate video'),input:label=>root.all().find(e=>e.tag==='label'&&e.textContent===label).children[0],form:root.all().find(e=>e.tag==='form'),save:root.all().find(e=>e.tag==='button'&&e.textContent==='Save changes'),video:()=>root.all().find(e=>e.tag==='video'),async poll(){timers[0]();await flush();},queue(){jobs=[...jobs,{id:'new',kind:'brb',state:'running',sequence:2,duration:40,progress:20,design_snapshot:{name:'Next',duration_mode:'music'}}];}};
}
test('status polling preserves the exact video element being reviewed',async()=>{
 const ui=await fixture(),video=ui.video();video.currentTime=12;video.paused=false;
 await ui.poll();assert.equal(ui.video(),video);assert.equal(ui.video().currentTime,12);
 ui.queue();await ui.poll();assert.equal(ui.video(),video);assert.equal(ui.video().currentTime,12);
 assert.ok(ui.root.all().some(e=>e.tag==='progress'));
});


test('saving captures one snapshot and leaves later BRB edits unsaved',async()=>{
 const ui=await fixture({delaySave:true});
 ui.input('Message').value='FIRST';ui.form.listeners.input();
 const saving=ui.save.onclick();await ui.flush();
 ui.input('Message').value='LATEST';ui.form.listeners.input();
 const generating=ui.form.onsubmit({preventDefault(){}});
 ui.resolveSave();await saving;await generating;
 const saves=()=>ui.calls.filter(c=>c.path.startsWith('/api/generator/designs')&&c.method!=='GET');
 assert.equal(saves().length,1);assert.equal(JSON.parse(saves()[0].body).scenes[0].text,'FIRST');
 assert.equal(ui.calls.filter(c=>c.path==='/api/generator/jobs'&&c.method==='POST').length,0);
 assert.equal(ui.save.disabled,false);
 await ui.save.onclick();assert.equal(saves().length,2);assert.equal(JSON.parse(saves()[1].body).scenes[0].text,'LATEST');
 await ui.form.onsubmit({preventDefault(){}});
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

 test('BRB results identify captured inputs and older snapshots after a later explicit save',async()=>{
 const ui=await fixture();await ui.save.onclick();await ui.form.onsubmit({preventDefault(){}});
 assert.ok(ui.root.all().some(e=>e.textContent.includes('Matches current design')));
 assert.ok(ui.root.all().some(e=>e.textContent.includes('No music')));
 assert.ok(ui.root.all().some(e=>e.textContent.includes('2026')));
 const reviewed=ui.video();ui.input('Message').value='NEWER DRAFT';ui.form.listeners.input();await ui.save.onclick();
 assert.ok(ui.root.all().some(e=>e.textContent.includes('Based on earlier changes')));
 assert.equal(ui.video(),reviewed,'draft status updates must not interrupt exact preview');
 assert.ok(ui.root.all().some(e=>e.textContent.includes('First retained render')));
 });

test('editing a BRB design does not write a revision until explicitly saved',async()=>{
 const ui=await fixture();
 ui.input('Message').value='A deliberate edit';ui.form.listeners.input();
 await new Promise(resolve=>setTimeout(resolve,1000));
 const writes=()=>ui.calls.filter(call=>call.path.startsWith('/api/generator/designs')&&call.method!=='GET');
 assert.equal(writes().length,0,'editing alone must not persist a design revision');
 await ui.save.onclick();assert.equal(writes().length,1);
 await ui.save.onclick();assert.equal(writes().length,1,'saving unchanged content must not create another revision');
});


test('reverted changes need no save and generation never saves implicitly',async()=>{
 const ui=await fixture();await ui.save.onclick();
 assert.equal(ui.save.disabled,true);assert.equal(!!ui.save.hidden,false);
 assert.equal(ui.generate.hidden,true,'current ready video needs no generation action');
 ui.input('Message').value='Changed';ui.form.listeners.input();
 assert.equal(ui.save.disabled,false);assert.equal(ui.generate.disabled,true);assert.equal(ui.generate.hidden,false);
 await ui.form.onsubmit({preventDefault(){}});
 assert.equal(ui.calls.filter(c=>c.method==='PUT'||c.path==='/api/generator/jobs'&&c.method==='POST').length,0);
 ui.input('Message').value='BE RIGHT BACK';ui.form.listeners.input();
 assert.equal(ui.save.disabled,true);assert.equal(ui.generate.hidden,true);
});

test('failed saves retain edits and generation stays blocked until explicit retry',async()=>{
 const ui=await fixture({failSave:true});
 ui.input('Message').value='Keep this';ui.form.listeners.input();
 await ui.save.onclick();assert.equal(ui.input('Message').value,'Keep this');
 assert.equal(ui.save.disabled,false);assert.equal(ui.generate.disabled,true);
 assert.ok(ui.root.all().some(e=>e.textContent.includes('Your edits are retained')));
 await ui.save.onclick();assert.equal(ui.save.disabled,true);assert.equal(ui.generate.disabled,false);
});

test('edits during job creation stay unsaved after the response arrives',async()=>{
 const ui=await fixture({delayJob:true});await ui.save.onclick();
 const generating=ui.form.onsubmit({preventDefault(){}});await ui.flush();
 ui.input('Message').value='After generation started';ui.form.listeners.input();
 ui.resolveJob();await generating;
 assert.equal(ui.input('Message').value,'After generation started');assert.equal(ui.save.disabled,false);assert.equal(ui.generate.disabled,true);
 await ui.save.onclick();
 const saves=ui.calls.filter(c=>c.path.startsWith('/api/generator/designs')&&c.method!=='GET');
 assert.equal(saves.length,2);assert.equal(JSON.parse(saves[1].body).scenes[0].text,'After generation started');
});
