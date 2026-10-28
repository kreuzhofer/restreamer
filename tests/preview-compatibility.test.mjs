import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

function fixture({standard=false, managed=true, streaming=true, supported=true, finite=false}={}) {
  const nodes=new Map(), sources=[], urls=new Map(); let reads=0, plays=0, remoteAtAttach;
  class Buffer extends EventTarget {
    constructor(){super();this.end=0;this.buffered={get length(){return 1;},start:()=>0,end:()=>this.end};}
    appendBuffer(){this.end+=2;queueMicrotask(()=>this.dispatchEvent(new Event('updateend')));}
    remove(){queueMicrotask(()=>this.dispatchEvent(new Event('updateend')));}
  }
  class Source extends EventTarget {
    static isTypeSupported(){return supported;}
    constructor(){super();this.buffer=new Buffer();sources.push(this);}
    addSourceBuffer(){return this.buffer;}
    endOfStream(){this.ended=true;}
  }
  class ManagedSource extends Source {constructor(){super();this.streaming=streaming;}}
  class Node extends EventTarget {
    constructor(){super();this.dataset={};this.disabled=false;this.textContent='';this.currentTime=0;this.paused=true;}
    set src(value){this.source=urls.get(value);remoteAtAttach=this.disableRemotePlayback;queueMicrotask(()=>this.source.dispatchEvent(new Event('sourceopen')));}
    play(){plays++;this.paused=false;this.dispatchEvent(new Event('playing'));return Promise.resolve();}
    pause(){this.paused=true;}
    removeAttribute(){} load(){} setAttribute(){}
  }
  const node=id=>{if(!nodes.has(id))nodes.set(id,new Node());return nodes.get(id);};
  const window={addEventListener(){},...(standard?{MediaSource:Source}:{}),...(managed?{ManagedMediaSource:ManagedSource}:{})};
  const context={window,MediaSource:window.MediaSource,document:{querySelector:node},location:{origin:'http://localhost'},AbortController,DOMException,setTimeout,clearTimeout,
    URL:{createObjectURL(source){const id=`blob:${urls.size}`;urls.set(id,source);return id;},revokeObjectURL(){}},
    fetch:async (_url,{signal})=>({ok:true,headers:{get:()=> 'video/mp4; codecs="avc1.42E01E,mp4a.40.2"'},body:{getReader:()=>({
      read(){reads++;if(reads===1)return Promise.resolve({done:false,value:new Uint8Array([1])});if(finite)return Promise.resolve({done:true});return new Promise((resolve,reject)=>signal.addEventListener('abort',()=>reject(new DOMException('Stopped','AbortError')),{once:true}));},cancel:async()=>{}
    })}})};
  vm.createContext(context);vm.runInContext(readFileSync('internal/relay/web/preview.js','utf8'),context);
  const flush=async()=>{for(let i=0;i<4;i++)await new Promise(resolve=>setImmediate(resolve));};
  return {window,node,sources,Source,ManagedSource,flush,get reads(){return reads;},get plays(){return plays;},get remoteAtAttach(){return remoteAtAttach;},async stop(){window.broadcastPreview.update(false);await flush();}};
}

test('live preview supports a ManagedMediaSource-only browser and disables remote playback before attachment',async()=>{
  const ui=fixture();ui.window.broadcastPreview.update(true);await ui.flush();
  try {assert.equal(ui.node('#broadcast-toggle').disabled,false);assert.ok(ui.sources[0] instanceof ui.ManagedSource);assert.equal(ui.remoteAtAttach,true);assert.ok(ui.plays>0);assert.match(ui.node('#broadcast-status').textContent,/Broadcast-source preview/);} finally {await ui.stop();}
});
test('desktop preview retains standard MediaSource when both APIs exist',async()=>{
  const ui=fixture({standard:true});ui.window.broadcastPreview.update(true);await ui.flush();
  try {assert.ok(ui.sources[0] instanceof ui.Source);assert.equal(ui.sources[0] instanceof ui.ManagedSource,false);assert.notEqual(ui.remoteAtAttach,true);assert.ok(ui.plays>0);} finally {await ui.stop();}
});
test('managed preview waits for browser demand and can stop while waiting',async()=>{
  const ui=fixture({streaming:false});ui.window.broadcastPreview.update(true);await ui.flush();
  try {assert.equal(ui.reads,0);assert.ok(ui.sources[0]);ui.sources[0].streaming=true;ui.sources[0].dispatchEvent(new Event('startstreaming'));await ui.flush();assert.ok(ui.reads>0);} finally {await ui.stop();}
  const idle=fixture({streaming:false});idle.window.broadcastPreview.update(true);await idle.flush();await idle.stop();assert.equal(idle.reads,0);assert.equal(idle.node('#broadcast-status').textContent,'Broadcast is off');
});
test('exact revision preview also supports ManagedMediaSource and finishes once',async()=>{
  const ui=fixture({finite:true});await ui.window.previewCandidate('test-revision');await ui.flush();
  assert.ok(ui.sources[0] instanceof ui.ManagedSource);assert.equal(ui.remoteAtAttach,true);assert.equal(ui.sources[0].ended,true);assert.ok(ui.plays>0);
});
test('unsupported media APIs or codecs remain explicit',async()=>{
  const absent=fixture({managed:false});absent.window.broadcastPreview.update(true);assert.equal(absent.node('#broadcast-toggle').disabled,true);assert.match(absent.node('#broadcast-status').textContent,/Media Source/);
  const unsupported=fixture({supported:false});unsupported.window.broadcastPreview.update(true);await unsupported.flush();assert.match(unsupported.node('#broadcast-status').textContent,/codec\/profile/);await unsupported.stop();
});
