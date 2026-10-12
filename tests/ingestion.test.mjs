import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

async function settings({clipboard = async () => {}, fail = false, pendingKey} = {}) {
  const buttons = ['reveal', 'copy', 'regenerate', 'address'].map(action => ({dataset: {ingestion: action}, disabled: false}));
  const field = {type: 'password', value: '••••••••••••••••'};
  const feedback = {textContent: ''};
  const address = {value: ''};
  const guidance = {textContent: ''};
  const calls = [], documentListeners = {}, observers = [];
  let click;
  const container = {dataset: {}, innerHTML: '', querySelectorAll: () => buttons,
    querySelector: selector => selector === '.ingestion-key' ? field : selector === '.ingestion-feedback' ? feedback : selector === '.ingestion-address' ? address : selector === '.ingestion-address-help' ? guidance : buttons[0],
    addEventListener: (name, callback) => { if (name === 'click') click = callback; }};
  const document = {body:{dataset:{workspace:'settings'}}, hidden:false, querySelectorAll:()=>[], addEventListener:(name,fn)=>{documentListeners[name]=fn;}};
  const window = {addEventListener(){}};
  const context = vm.createContext({window, document, location:{origin:'http://127.0.0.1:18849'}, navigator:{clipboard:{writeText:clipboard}}, confirm:()=>true,
    URL, AbortSignal, MutationObserver: class {constructor(fn){observers.push(fn);} observe(){}},
    fetch:async (path,options)=>{assert.ok(path.startsWith('http://127.0.0.1:18849/')); path=new URL(path).pathname; calls.push([path, options]);return {ok:!fail,text:async()=> 'Service unavailable',json:async()=> path.endsWith('/key') && pendingKey ? pendingKey : path.endsWith('/key') ? {key:'dummy-private-ingestion-key',revision:'v1'} : {managed:true,revision:'v1',application:'live'}};}});
  vm.runInContext(readFileSync(new URL('../internal/relay/web/ingestion.js',import.meta.url),'utf8'),context);
  await window.mountIngestionSettings(container);
  return {field,feedback,calls,document,address,async click(action){const button=buttons.find(b=>b.dataset.ingestion===action);await click({target:{closest:()=>button}});},leave(){document.body.dataset.workspace='live';observers.forEach(fn=>fn());}};
}

test('Settings loads metadata only; reveal is explicit and leaving workspace discards plaintext',async()=>{
 const ui=await settings();
 assert.equal(ui.calls.length,1);assert.equal(ui.calls[0][0],'/api/ingestion');assert.equal(ui.field.type,'password');
 await ui.click('reveal');assert.equal(ui.field.type,'text');assert.equal(ui.field.value,'dummy-private-ingestion-key');
 ui.leave();assert.equal(ui.field.type,'password');assert.ok(!ui.field.value.includes('dummy-private'));
});

test('clipboard failures give manual-copy guidance without exposing key',async()=>{
 const ui=await settings({clipboard:async()=>{throw new Error('denied');}});
 await ui.click('copy');assert.match(ui.feedback.textContent,/Could not copy.*Reveal key/);assert.equal(ui.field.type,'password');
});

test('copy succeeds only after explicit authenticated control request',async()=>{
 let copied='';const ui=await settings({clipboard:async key=>{copied=key;}});await ui.click('copy');
 assert.equal(copied,'dummy-private-ingestion-key');assert.match(ui.feedback.textContent,/copied/);
 assert.equal(ui.calls[1][1].headers['X-Restreamer-Control'],'1');assert.equal(ui.calls[1][1].method,'POST');
});


test('a late reveal response cannot reinsert plaintext after leaving Settings',async()=>{
 let release;const pendingKey=new Promise(resolve=>{release=resolve;});const ui=await settings({pendingKey});
 const operation=ui.click('reveal');ui.leave();release({key:'late-secret',revision:'v1'});await operation;
 assert.equal(ui.field.type,'password');assert.ok(!ui.field.value.includes('late-secret'));
});

test('server address copying requires an explicit RTMP host, port and application without a key',async()=>{
 let copied='';const ui=await settings({clipboard:async value=>{copied=value;}});
 ui.address.value='rtmp://publisher.example:1940/live';await ui.click('address');assert.equal(copied,ui.address.value);
 assert.equal(ui.calls.length,1); // Copying an address never retrieves the ingestion credential.
 copied='';ui.address.value='rtmp://publisher.example/live/private-key';await ui.click('address');assert.equal(copied,'');
 assert.match(ui.feedback.textContent,/published port/);
});
