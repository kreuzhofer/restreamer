import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

test('BRB theme polling displays capacity and resumed-attempt messages for the same job',async()=>{
 const nodes=new Map(), timers=[];let preparation={id:'job',state:'running',message:'Preparing captured BRB theme.'};
 const node=id=>{if(id==='brb-theme-select')return null;if(!nodes.has(id))nodes.set(id,{textContent:'',hidden:false,addEventListener(){},pause(){},removeAttribute(){},load(){},dataset:{}});return nodes.get(id);};
 const window={};
 const context={window,document:{getElementById:node},matchMedia:()=>({matches:true,addEventListener(){}}),AbortSignal,URL,location:{origin:'http://localhost'},setInterval:fn=>timers.push(fn),fetch:async url=>({ok:true,json:async()=>url.pathname.endsWith('/preparation')?{...preparation}:null})};
 vm.createContext(context);vm.runInContext(readFileSync('internal/relay/web/brb-theme.js','utf8'),context);
 const flush=()=>new Promise(resolve=>setImmediate(resolve));
 window.brbThemes.update({brb_assets:{generation:'saved',profile:{width:1920,height:1080,fps:25}}},true,false,false);await flush();
 preparation.message='Waiting for broadcast capacity; rendering restarts from the beginning after Stop.';
 timers[0]();await flush();assert.equal(node('brb-theme-status').textContent,preparation.message);
 preparation.message='Preparing captured BRB theme.';
 timers[0]();await flush();assert.equal(node('brb-theme-status').textContent,preparation.message);
});
