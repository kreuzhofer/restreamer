// Development-only visual harness. All relay responses are simulated; no RTMP.
import {createServer} from 'node:http';
import {readFile} from 'node:fs/promises';
import {spawn} from 'node:child_process';
import {WebSocketServer} from 'ws';
import {build} from 'esbuild';
const built=await build({entryPoints:['src/keys.ts'],bundle:true,platform:'node',format:'esm',write:false});
const {keySVG,health}=await import('data:text/javascript;base64,'+Buffer.from(built.outputFiles[0].text).toString('base64'));
const port=18903,uuid='com.kreuzhofer.restreamer';
const config={endpoint:`http://127.0.0.1:${port}`,username:'preview',password:'dummy-preview',allowInsecureHttp:true};
const clips=[{id:'a'.repeat(64),name:'Workshop tour',state:'ready',duration:42},{id:'b'.repeat(64),name:'Printing timelapse',state:'ready',duration:86}];
const snapshot={stage:{stage:'BRB',source:'brb',mode:'real',server_id:'preview',context:'preview:1',media:{},outcomes:[]},outputs:[{name:'A',enabled:true,can_enable:true,state:'streaming'},{name:'B',enabled:true,can_enable:true,state:'streaming'}]};
let plugin,inspector;
const samples=[['Healthy BRB',snapshot],['Partial live',{...snapshot,stage:{...snapshot.stage,stage:'LIVE',source:'obs'},outputs:[snapshot.outputs[0],{...snapshot.outputs[1],state:'retrying'}]}],['No delivery',{...snapshot,outputs:snapshot.outputs.map(o=>({...o,state:'retrying'}))}],['Starting',{...snapshot,outputs:snapshot.outputs.map(o=>({...o,state:'waiting_for_keyframe'}))}],['Stopped',{...snapshot,stage:{...snapshot.stage,stage:'OFF'}}],['No destinations',{...snapshot,outputs:[]}],['Disconnected',undefined]];
const server=createServer(async(req,res)=>{
  if(req.url==='/api/dashboard'){res.setHeader('Content-Type','application/json');res.end(JSON.stringify(snapshot));return;}
  if(req.url==='/api/library'){res.setHeader('Content-Type','application/json');res.end(JSON.stringify({enabled:true,revisions:clips}));return;}
  if(req.url==='/'){
    res.setHeader('Content-Type','text/html');res.end(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Stream Deck verification</title><style>body{background:#17191c;color:#f0f1f3;font:15px/1.5 system-ui;margin:24px}h1{font-size:24px}p{color:#b2b8c1}main{display:flex;gap:40px;flex-wrap:wrap}section{max-width:640px}.keys{display:flex;flex-wrap:wrap;gap:24px}figure{margin:0;width:110px}figcaption{font-size:12px;margin-top:8px}img{width:72px;height:72px;display:block}.large img{width:96px;height:96px}iframe{border:1px solid #636c78;width:360px;height:870px;max-width:100%}</style><h1>Stream Deck visual verification</h1><p>Simulated relay only. Keys shown at their actual 72px and 96px sizes; inspector uses the shipping UI.</p><main><section><h2>72px keys</h2><div class="keys">${samples.map(([label,state])=>`<figure><img alt="${label}" src="data:image/svg+xml;base64,${Buffer.from(keySVG(health(state))).toString('base64')}"><figcaption>${label}</figcaption></figure>`).join('')}</div><h2>96px keys</h2><div class="keys large">${samples.slice(0,4).map(([label,state])=>`<figure><img alt="${label}" src="data:image/svg+xml;base64,${Buffer.from(keySVG(health(state))).toString('base64')}"><figcaption>${label}</figcaption></figure>`).join('')}</div></section><section><h2>Play clip settings</h2><iframe title="Shipping property inspector" src="/ui/inspector.html" onload='this.contentWindow.connectElgatoStreamDeckSocket("${port}","clip","registerPropertyInspector","{}",JSON.stringify({action:"${uuid}.play-clip",payload:{settings:{revision:"${clips[0].id}",name:"Workshop tour"}}}))'></iframe></section></main></html>`);return;
  }
  if(['/ui/inspector.html','/ui/inspector.js','/ui/inspector.css'].includes(req.url)){
    res.setHeader('Content-Type',req.url.endsWith('.html')?'text/html':req.url.endsWith('.js')?'text/javascript':'text/css');
    res.end(await readFile(new URL('../com.kreuzhofer.restreamer.sdPlugin'+req.url,import.meta.url)));return;
  }
  res.writeHead(404).end();
});
const ws=new WebSocketServer({server});
ws.on('connection',socket=>socket.on('message',raw=>{
  const msg=JSON.parse(raw);
  if(msg.event==='registerPlugin'){
    plugin=socket;
    socket.send(JSON.stringify({event:'willAppear',context:'clip',action:uuid+'.play-clip',device:'device',payload:{controller:'Keypad',coordinates:{column:0,row:0},settings:{revision:clips[0].id,name:clips[0].name}}}));
  }else if(msg.event==='getGlobalSettings')socket.send(JSON.stringify({event:'didReceiveGlobalSettings',id:msg.id,payload:{settings:config}}));
  else if(msg.event==='registerPropertyInspector'){
    inspector=socket;plugin?.send(JSON.stringify({event:'propertyInspectorDidAppear',context:'clip',action:uuid+'.play-clip',device:'device'}));
  }else if(msg.event==='sendToPlugin')plugin?.send(JSON.stringify(msg));
  else if(msg.event==='sendToPropertyInspector')inspector?.send(JSON.stringify(msg));
  else if(msg.event==='setSettings'){
    plugin?.send(JSON.stringify({event:'didReceiveSettings',context:'clip',action:uuid+'.play-clip',payload:{settings:msg.payload}}));
  }else if(msg.event==='setGlobalSettings')Object.assign(config,msg.payload);
}));
server.listen(port,'127.0.0.1',()=>{
  const info={application:{version:'7.1.0',platform:'mac',platformVersion:'13.0',language:'en',font:'Arial'},devices:[{id:'device',type:0,name:'Preview',size:{columns:5,rows:3}}],plugin:{uuid,version:'1.0.0'}};
  const child=spawn(process.execPath,['bin/plugin.js','-port',String(port),'-pluginUUID','preview','-registerEvent','registerPlugin','-info',JSON.stringify(info)],{cwd:new URL('../com.kreuzhofer.restreamer.sdPlugin/',import.meta.url),stdio:'inherit'});
  process.on('SIGTERM',()=>{child.kill();process.exit();});process.on('SIGINT',()=>{child.kill();process.exit();});
  console.log(`Visual harness: http://127.0.0.1:${port}`);
});
