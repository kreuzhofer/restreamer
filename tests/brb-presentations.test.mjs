import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
async function fixture() {
 class Element {
  constructor(tag,text=''){this.tag=tag;this.textContent=text;this.children=[];this.value='';this.classList={add(){}};}
  append(...children){for(const child of children){child.parent=this;this.children.push(child);}}
  prepend(child){child.parent=this;this.children.unshift(child);}
  replaceChildren(...children){this.children=[];this.append(...children);}
  replaceWith(child){const at=this.parent.children.indexOf(this);this.parent.children[at]=child;child.parent=this.parent;}
  remove(){this.parent.children=this.parent.children.filter(c=>c!==this);}
  setAttribute(){}
  addEventListener(){}
  closest(){return null;}
  all(){return [this,...this.children.flatMap(c=>c.all())];}
 }
 const root=new Element('section'),timers=[];
 let jobs=[{id:'ready',kind:'brb',state:'ready',sequence:1,duration:30,media_revision:'revision',design_snapshot:{name:'Workshop',duration_mode:'music'}}];
 const window={addEventListener(){},confirm:()=>true};
 const context={window,document:{getElementById:()=>root,createElement:tag=>new Element(tag)},Option:class extends Element {constructor(label,value){super('option',label);this.value=value;}},URL,AbortSignal,location:{origin:'http://localhost:18871'},setInterval:fn=>timers.push(fn),fetch:async url=>({ok:true,json:async()=>new URL(url).pathname==='/api/generator/jobs'?jobs:[]})};
 vm.createContext(context);
 for(const name of ['music-selection','brb-presentations']) vm.runInContext(readFileSync(`internal/relay/web/${name}.js`,'utf8'),context);
 const flush=()=>new Promise(resolve=>setImmediate(resolve));await flush();
 return {root,video:()=>root.all().find(e=>e.tag==='video'),async poll(){timers[0]();await flush();},queue(){jobs=[...jobs,{id:'new',kind:'brb',state:'running',sequence:2,duration:40,progress:20,design_snapshot:{name:'Next',duration_mode:'music'}}];}};
}
test('status polling preserves the exact video element being reviewed',async()=>{
 const ui=await fixture(),video=ui.video();video.currentTime=12;video.paused=false;
 await ui.poll();assert.equal(ui.video(),video);assert.equal(ui.video().currentTime,12);
 ui.queue();await ui.poll();assert.equal(ui.video(),video);assert.equal(ui.video().currentTime,12);
 assert.ok(ui.root.all().some(e=>e.tag==='progress'));
});
