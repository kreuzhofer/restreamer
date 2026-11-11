import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import {readFileSync} from 'node:fs';

test('creating a show uses the current edited template version', async () => {
  class Element {
    constructor(tag = 'div') { this.tag = tag; this.children = []; this.events = {}; this.hidden = false; }
    append(...items) { this.children.push(...items); }
    replaceChildren(...items) { this.children = items; }
    addEventListener(event, action) { this.events[event] = action; }
    querySelector(tag) { return this.children.find(item => item.tag === tag) || this.children.map(item => item.querySelector(tag)).find(Boolean); }
  }
  const nodes = new Map();
  const get = id => { if (!nodes.has(id)) nodes.set(id,new Element()); return nodes.get(id); };
  const calls = [];
  const location = {href:'http://localhost/library?collection=packages'};
  const context = {
    document:{getElementById:get,createElement:tag => new Element(tag)},
    window:{addEventListener() {}}, URL, location,
    fetch:async (url, options) => {
      calls.push({url,options});
      const data = url === '/api/generator/packages' ? [{id:'kit',version:1,name:'Kit',assets:[],themes:[],templates:[{id:'template',name:'Welcome',theme:'theme'}]}]
        : url.endsWith('/designs') ? {id:'new-design'} : {id:'template',name:'Edited welcome',version:2};
      return {ok:true,json:async () => data};
    },
  };
  vm.runInNewContext(readFileSync(new URL('../internal/relay/web/brand-packages.js',import.meta.url),'utf8'),context);
  await new Promise(resolve => setImmediate(resolve));
  const section = get('brand-package-list').children[0];
  const button = section.children.flatMap(item => item.children).find(item => item.textContent === 'Create Welcome');
  assert.ok(button);
  await button.events.click();
  const copy = calls.find(call => call.url.endsWith('/designs'));
  assert.equal(JSON.parse(copy.options.body).version,2);
  assert.equal(JSON.parse(copy.options.body).theme.revision,1);
  assert.match(location.href,/editor\?id=new-design/);
});
