import {createHash} from 'node:crypto';
import {readFile,writeFile} from 'node:fs/promises';
const root=new URL('../../',import.meta.url);
const manifestURL=new URL('internal/relay/artwork/maker-workshop/manifest.json',root);
const seasons=['workshop','halloween','christmas'],stages=['brb','prestream','ending'];
const files=['artwork/maker-workshop/build.py','artwork/maker-workshop/render.py','artwork/brb/font.json'];
for(const name of ['workshop.png','halloween.png','christmas.png','robot-parts.png','sprite-bounds.json','seasonal-parts.png','seasonal-bounds.json','prompts.json','original-robot-prompts.json']) files.push('artwork/maker-workshop/assets/'+name);
for(const season of seasons) for(const stage of stages) for(const extension of ['mp4','png']) files.push(`internal/relay/artwork/maker-workshop/${season}/${stage}.${extension}`);
const hashes={};
for(const file of files) hashes[file]=createHash('sha256').update(await readFile(new URL(file,root))).digest('hex');
if(process.argv.includes('--record')){
 await writeFile(manifestURL,JSON.stringify({width:2048,height:1152,source_width:1024,source_height:576,fps:24,duration:12,stages:{brb:'loop',prestream:'loop',ending:'hold'},hashes},null,2)+'\n');
}else{
 const manifest=JSON.parse(await readFile(manifestURL,'utf8'));
 if(JSON.stringify(manifest.hashes)!==JSON.stringify(hashes)) throw Error('Workshop source or prepared media changed. Rebuild, verify every stage, then record the manifest.');
 console.log('All nine workshop stage masters and their sources match the manifest.');
}
