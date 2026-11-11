import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
function fixture(){
 class Element {constructor(){this.hidden=false;this.value='';this.children=[];this.dataset={};this.handlers={};this.attrs={};}addEventListener(k,f){this.handlers[k]=f;}setAttribute(k,v){this.attrs[k]=v;}focus(){this.focused=true;}}
 const ids=Object.fromEntries(['music-tracks-view','music-playlists','music-search','music-search-status','music-upload-open','music-playlist-create','music-list','music-playlist-list'].map(id=>[id,new Element()]));
 ids['music-list'].children=Array.from({length:100},(_,i)=>({textContent:`Track ${i+1}`,hidden:false}));ids['music-playlist-list'].children=[{textContent:'Workshop radio',hidden:false},{textContent:'After hours',hidden:false}];
 const tabs=['tracks','playlists'].map(view=>Object.assign(new Element(),{dataset:{musicView:view}}));
 const window={musicPlaylists:{create(){window.created=true;}}};
 const root={querySelector:s=>ids[s.slice(1)],querySelectorAll:()=>tabs};
 const context={window,document:{getElementById:id=>id==='studio-music'?root:ids[id]},URL,location:{href:'http://localhost/library?collection=music'},history:{replaceState(){}},MutationObserver:class{observe(){}},};
 vm.runInNewContext(readFileSync('internal/relay/web/music-browser.js','utf8'),context);
 return {ids,tabs,window};
}
test('playlists are one switch away even with 100 tracks and search targets the selected view',()=>{
 const {ids,tabs,window}=fixture();assert.equal(ids['music-playlists'].hidden,true);
 tabs[1].handlers.click();assert.equal(ids['music-tracks-view'].hidden,true);assert.equal(ids['music-playlists'].hidden,false);assert.equal(ids['music-upload-open'].hidden,true);assert.equal(ids['music-playlist-create'].hidden,false);
 ids['music-search'].value='workshop';ids['music-search'].handlers.input();assert.deepEqual(ids['music-playlist-list'].children.map(r=>r.hidden),[false,true]);assert.equal(ids['music-list'].children.filter(r=>r.hidden).length,0);
 ids['music-playlist-create'].handlers.click();assert.equal(window.created,true);
 tabs[0].handlers.click();assert.equal(ids['music-search'].value,'');tabs[1].handlers.click();assert.equal(ids['music-search'].value,'workshop');
});
