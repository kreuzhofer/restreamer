import test from 'node:test';
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {spawn} from 'node:child_process';
import {once} from 'node:events';
import {readdir, readFile} from 'node:fs/promises';
import {WebSocketServer} from 'ws';

const uuid = 'com.kreuzhofer.restreamer';
const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
async function eventually(predicate, timeout = 5000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) { if (predicate()) return; await wait(20); }
  assert.ok(predicate(), 'Expected observable plugin behavior before deadline');
}

// The two external boundaries: Stream Deck events/render commands and HTTP.
async function rig(t) {
  const commands = [], requests = [], messages = [];
  const state = {
    stage: {stage:'BRB', source:'brb', mode:'real', server_id:'server-1', context:'server-1:1', media:{}, outcomes:[], return_stage:'LIVE'},
    outputs:[{name:'local', enabled:true, can_enable:true, state:'streaming'}]
  };
  const api = {state, commands, requests, status:200, delay:0,
    library:{enabled:true, files:[], revisions:[{id:'a'.repeat(64),name:'Hello',state:'ready',duration:10},{id:'b'.repeat(64),name:'Goodbye',state:'ready',duration:12}]}};
  const server = createServer(async (req, res) => {
    let body = ''; for await (const chunk of req) body += chunk;
    requests.push({path:req.url, auth:req.headers.authorization, headers:req.headers});
    if (api.delay) await wait(api.delay);
    res.setHeader('Content-Type','application/json');
    if (req.headers.authorization !== 'Basic ' + Buffer.from('test:dummy-password').toString('base64')) {res.writeHead(401).end('{}');return;}
    if (api.status !== 200) {res.writeHead(api.status).end('{}');return;}
    if (req.url === '/api/stage/commands') {
      const cmd = JSON.parse(body); commands.push(cmd);
      const result = api.result || {id:cmd.id,state:'completed'};
      res.end(JSON.stringify({...result,id:cmd.id}));
    } else if (req.url === '/api/library') res.end(JSON.stringify(api.library));
    else res.end(JSON.stringify(state));
  });
  server.listen(0,'127.0.0.1'); await once(server,'listening');
  const ws = new WebSocketServer({port:0,host:'127.0.0.1'}); await once(ws,'listening');
  const settings = {endpoint:`http://127.0.0.1:${server.address().port}`,username:'test',password:'dummy-password',allowInsecureHttp:true};
  let socket, output='';
  const connection = once(ws,'connection');
  const child = spawn(process.execPath,['bin/plugin.js','-port',String(ws.address().port),'-pluginUUID','plugin-test','-registerEvent','registerPlugin','-info',JSON.stringify({application:{version:'7.1.0',platform:'mac',platformVersion:'13.0',language:'en',font:'Arial'},devices:[{id:"device",type:0,name:"Test Deck",size:{columns:5,rows:3}}],plugin:{uuid,version:'1.0.0'}})],{cwd:new URL('../com.kreuzhofer.restreamer.sdPlugin/',import.meta.url),stdio:['ignore','pipe','pipe']});
  child.stdout.on('data', b=>output+=b); child.stderr.on('data',b=>output+=b);
  t.after(async()=>{child.kill();socket?.close();ws.close();server.closeAllConnections();server.close();assert.doesNotMatch(output,/dummy-password|Basic dGVz/);const dir=new URL('../com.kreuzhofer.restreamer.sdPlugin/logs/',import.meta.url);for(const file of await readdir(dir)){assert.doesNotMatch(await readFile(new URL(file,dir),'utf8'),/dummy-password|Basic dGVz/);}});
  [socket] = await Promise.race([connection,wait(5000).then(()=>{throw new Error('Plugin did not connect: '+output);})]);
  const send = (event, context, action, payload={}) => socket.send(JSON.stringify({event,context,action:`${uuid}.${action}`,device:'device',payload}));
  socket.on('message', raw=>{
    const msg=JSON.parse(raw); messages.push(msg);
    if (msg.event === 'getGlobalSettings') socket.send(JSON.stringify({event:'didReceiveGlobalSettings',payload:{settings}}));
  });
  const appear=(kind,context=kind,settings={})=>send('willAppear',context,kind,{settings,controller:'Keypad',coordinates:{column:0,row:0},state:0});
  const key=(kind,context=kind,event='keyDown')=>send(event,context,kind,{settings:{},controller:'Keypad',coordinates:{column:0,row:0},state:0});
  const image=context=>{
    const m=messages.findLast(m=>m.event==='setImage'&&m.context===context);
    if(!m)return '';
    const value=m.payload.image;
    return value.startsWith('data:image/svg+xml;base64,')?Buffer.from(value.split(',')[1],'base64').toString():decodeURIComponent(value);
  };
  return {...api, api, state, commands, requests,messages,send,appear,key,image,settings};
}

test('a configured Prestream key sends an authenticated, identified server command',async t=>{
  const r=await rig(t);r.appear('prestream');
  await eventually(()=>r.image('prestream').includes('READY'));
  r.key('prestream');
  await eventually(()=>r.commands.length===1);
  assert.deepEqual({...r.commands[0],id:'ID'},{id:'ID',server_id:'server-1',context:'server-1:1',action:'prestream',confirmed:true});
  assert.ok(r.commands[0].id.length>=16);
  assert.equal(r.requests.find(r=>r.path==='/api/stage/commands').headers['x-restreamer-control'],'1');
});

test('status key reports stage, eligible delivery counts, startup and loss without stale green',async t=>{
  const r=await rig(t);r.appear('status');
  await eventually(()=>r.image('status').includes('1/1'));
  assert.match(r.image('status'),/BRB/);assert.match(r.image('status'),/HEALTHY/);
  r.state.stage.stage='LIVE';r.state.stage.source='obs';
  r.state.outputs.push({name:'failed',enabled:true,can_enable:true,state:'retrying'});
  r.state.outputs.push({name:'disabled',enabled:false,can_enable:true,state:'retrying'});
  await eventually(()=>r.image('status').includes('1/2'));
  assert.match(r.image('status'),/DEGRADED/);
  r.state.outputs[0].state='retrying';
  await eventually(()=>r.image('status').includes('FAILED'));
  r.state.outputs=[{name:'new',enabled:true,can_enable:true,state:'waiting_for_keyframe'}];
  await eventually(()=>r.image('status').includes('STARTING'));
  r.state.stage.stage='OFF';
  await eventually(()=>r.image('status').includes('OFF'));
  assert.doesNotMatch(r.image('status'),/HEALTHY/);
  r.api.status=401;
  await eventually(()=>r.image('status').includes('UNKNOWN'));
  r.key('status');await eventually(()=>r.messages.some(m=>m.event==='openUrl'));
  assert.equal(r.messages.find(m=>m.event==='openUrl').payload.url,r.settings.endpoint+'/live');
  r.api.status=200;r.state.stage.stage='BRB';r.state.outputs=[];
  await eventually(()=>r.image('status').includes('NO DEST'));
  assert.doesNotMatch(r.image('status'),/HEALTHY/);
});

test('stage controls, independent exact clip selections, and server-confirmed active keys',async t=>{
  const r=await rig(t);
  for(const kind of ['go-live','brb-return','end-stream'])r.appear(kind);
  r.appear('play-clip','clip-a',{revision:'a'.repeat(64),name:'Hello'});
  r.appear('play-clip','clip-b',{revision:'b'.repeat(64),name:'Goodbye'});
  await eventually(()=>r.image('brb-return').includes('RETURN'));
  for(const [kind,context,expected] of [['go-live','go-live','go_live'],['brb-return','brb-return','return'],['end-stream','end-stream','end_stream'],['play-clip','clip-a','play_clip'],['play-clip','clip-b','play_clip']]){
    const before=r.commands.length;r.key(kind,context);
    await eventually(()=>r.commands.length===before+1);
    assert.equal(r.commands.at(-1).action,expected);
    r.key(kind,context,'keyUp');await wait(1100);
  }
  assert.equal(r.commands[3].revision,'a'.repeat(64));assert.equal(r.commands[4].revision,'b'.repeat(64));
  r.state.stage.stage='CLIP';r.state.stage.source='file';r.state.stage.playback={state:'playing'};r.state.stage.media={revision:'b'.repeat(64)};
  await eventually(()=>r.image('clip-b').includes('ACTIVE'));
  assert.doesNotMatch(r.image('clip-a'),/ACTIVE/);
  r.state.stage.stage='LIVE';await eventually(()=>!r.image('brb-return').includes('RETURN'));r.key('brb-return');
  await eventually(()=>r.commands.at(-1).action==='brb');
});

test('duplicate presses are suppressed and Stop now requires hold then release',async t=>{
  const r=await rig(t);r.appear('prestream');r.appear('stop-now');
  await eventually(()=>r.image('prestream').includes('READY'));
  r.key('prestream');r.key('prestream');r.key('prestream');
  await eventually(()=>r.commands.length===1);await wait(1200);assert.equal(r.commands.length,1);
  r.key('stop-now');r.key('stop-now','stop-now','keyUp');await wait(200);assert.equal(r.commands.length,1);
  r.key('stop-now');await wait(2100);assert.equal(r.commands.length,1);
  r.key('stop-now','stop-now','keyUp');await eventually(()=>r.commands.length===2);
  assert.equal(r.commands[1].action,'stop_now');
});

test('pending Go live is not reissued, times out as NO OBS, and restart/network recovery never replays commands',async t=>{
  const r=await rig(t);r.appear('go-live');r.appear('status');
  await eventually(()=>r.image('go-live').includes('READY'));
  r.api.result={state:'pending'};r.key('go-live');await eventually(()=>r.commands.length===1);
  r.state.stage.pending={id:r.commands[0].id};
  r.state.stage.outcomes=[{id:r.commands[0].id,state:'pending'}];
  r.key('go-live','go-live','keyUp');await wait(1300);r.key('go-live');await wait(200);assert.equal(r.commands.length,1);
  r.state.stage.pending=undefined;r.state.stage.outcomes=[{id:r.commands[0].id,state:'timed_out',reason:'No fresh OBS keyframe'}];
  await eventually(()=>r.image('go-live').includes('NO OBS'));
  r.api.delay=4000;
  await eventually(()=>r.image('status').includes('UNKNOWN'),5000);
  r.key('go-live','go-live','keyUp');r.key('go-live');
  r.api.delay=0;r.state.stage.server_id='server-2';r.state.stage.context='server-2:1';r.state.stage.outcomes=[];
  await eventually(()=>r.image('status').includes('HEALTHY'),6500);
  assert.equal(r.commands.length,1);
});

test('clip inspector receives ready choices without credentials, and missing or changed selections do not play',async t=>{
  const r=await rig(t);r.appear('play-clip','clip',{revision:'a'.repeat(64),name:'Hello'});
  await eventually(()=>r.image('clip').includes('Hello'));
  r.send('propertyInspectorDidAppear','clip','play-clip');
  r.send('sendToPlugin','clip','play-clip',{command:'load'});
  await eventually(()=>r.messages.some(m=>m.event==='sendToPropertyInspector'&&m.payload.clips));
  const reply=r.messages.find(m=>m.event==='sendToPropertyInspector'&&m.payload.clips);
  assert.equal(reply.payload.clips.length,2);assert.doesNotMatch(JSON.stringify(reply),/dummy-password|Basic /);
  r.api.library.revisions[0].name='Changed';r.key('play-clip','clip');
  await eventually(()=>r.image('clip').includes('CHANGED'));assert.equal(r.commands.length,0);
  r.key('play-clip','clip','keyUp');await wait(1000);r.api.library.revisions=[];r.key('play-clip','clip');
  await eventually(()=>r.image('clip').includes('MISSING'));assert.equal(r.commands.length,0);
});

test('additional keys share status requests, authentication recovers, and changed Stop confirmation is cancelled',async t=>{
  const r=await rig(t);r.appear('status');r.appear('stop-now');
  await eventually(()=>r.image('status').includes('HEALTHY'));
  const before=r.requests.filter(x=>x.path==='/api/dashboard').length;
  for(let i=0;i<20;i++)r.appear('status','status-'+i);
  await wait(2200);
  assert.ok(r.requests.filter(x=>x.path==='/api/dashboard').length-before<=3,'twenty keys must not multiply polling');
  r.key('stop-now');r.state.stage.context='server-1:2';await wait(2100);r.key('stop-now','stop-now','keyUp');
  await eventually(()=>r.image('stop-now').includes('CHANGED'));assert.equal(r.commands.length,0);
  r.api.status=401;await eventually(()=>r.image('status').includes('AUTH'));
  r.api.status=200;await eventually(()=>r.image('status').includes('HEALTHY'));assert.equal(r.commands.length,0);
});

test('connection form rejects unsafe endpoints and stores secrets only in global settings',async t=>{
  const r=await rig(t);r.appear('status');await eventually(()=>r.image('status').includes('HEALTHY'));
  r.send('propertyInspectorDidAppear','status','status');
  r.send('sendToPlugin','status','status',{command:'save',settings:{endpoint:'https://user:secret@example.test',username:'test',password:'dummy-password'}});
  await eventually(()=>r.messages.some(m=>m.event==='sendToPropertyInspector'&&m.payload.error));
  assert.equal(r.messages.filter(m=>m.event==='setGlobalSettings').length,0);
  r.send('sendToPlugin','status','status',{command:'save',settings:r.settings});
  await eventually(()=>r.messages.some(m=>m.event==='setGlobalSettings'));
  assert.equal(r.messages.find(m=>m.event==='setGlobalSettings').payload.password,'dummy-password');
  assert.equal(r.messages.filter(m=>m.event==='setSettings').length,0);
  assert.doesNotMatch(JSON.stringify(r.messages.filter(m=>m.event==='sendToPropertyInspector')),/dummy-password/);
});

test('invalid imported key settings are isolated and a corrected selection recovers',async t=>{
  const r=await rig(t);
  r.appear('play-clip','bad-name',{revision:'a'.repeat(64),name:7});
  r.appear('play-clip','bad-revision',{revision:7,name:'Hello'});
  r.appear('play-clip','bad-shape',null);
  r.appear('status');r.appear('prestream');
  await eventually(()=>r.image('status').includes('HEALTHY'));
  for(const key of ['bad-name','bad-revision','bad-shape'])assert.match(r.image(key),/INVALID/);
  r.key('play-clip','bad-name');await wait(100);assert.equal(r.commands.length,0);
  r.send('didReceiveSettings','prestream','prestream',{settings:{name:[]}});
  await eventually(()=>r.image('prestream').includes('INVALID'));
  r.api.status=401;await eventually(()=>r.image('status').includes('AUTH'));
  r.api.status=200;await eventually(()=>r.image('status').includes('HEALTHY'));
  r.send('didReceiveSettings','bad-name','play-clip',{settings:{revision:'a'.repeat(64),name:'Hello'}});
  await eventually(()=>r.image('bad-name').includes('Hello'));
  r.key('play-clip','bad-name');await eventually(()=>r.commands.length===1);
  assert.equal(r.commands[0].revision,'a'.repeat(64));
});

test('active keys distinguish intended stage from fallback, paused, and failed playback',async t=>{
  const r=await rig(t);r.state.stage.stage='LIVE';r.state.stage.source='brb';
  r.appear('go-live');r.appear('status');r.appear('play-clip','clip',{revision:'a'.repeat(64),name:'Hello'});
  await eventually(()=>r.image('status').includes('DEGRADED'));
  assert.doesNotMatch(r.image('go-live'),/ACTIVE/);assert.match(r.image('go-live'),/NO OBS/);
  r.state.stage.source='obs';await eventually(()=>r.image('go-live').includes('ACTIVE'));
  r.state.stage.stage='CLIP';r.state.stage.source='file';r.state.stage.media={revision:'a'.repeat(64)};r.state.stage.playback={state:'playing'};
  await eventually(()=>r.image('clip').includes('ACTIVE'));
  r.state.stage.source='brb';r.state.stage.playback={state:'paused'};
  await eventually(()=>r.image('clip').includes('PAUSED'));assert.doesNotMatch(r.image('clip'),/ACTIVE/);
  r.state.stage.playback={state:'failed'};r.state.stage.error='Prepared video cannot play';
  await eventually(()=>r.image('clip').includes('FAILED'));assert.doesNotMatch(r.image('clip'),/ACTIVE/);
});
