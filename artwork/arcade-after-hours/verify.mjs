import {createHash} from 'node:crypto';
import {readFile,writeFile} from 'node:fs/promises';
const root=new URL('../../',import.meta.url);
const manifestURL=new URL('internal/relay/artwork/after-hours/manifest.json',root);
const files=['artwork/shared/build-palette-player.mjs','artwork/shared/render-palette-loop.mjs','artwork/arcade-after-hours/background.png','artwork/arcade-after-hours/player.template.html','artwork/arcade-after-hours/build.mjs','artwork/arcade-after-hours/render.mjs','artwork/arcade-after-hours/check-masks.mjs','internal/relay/artwork/after-hours/loop.mp4','internal/relay/artwork/after-hours/poster.png','internal/mediaauthor/pixel-font.json'];
const hashes={};
for(const file of files) hashes[file]=createHash('sha256').update(await readFile(new URL(file,root))).digest('hex');
if(process.argv.includes('--record')) {
 await writeFile(manifestURL,JSON.stringify({width:1920,height:1080,fps:30,duration:16,hashes},null,2)+'\n');
} else {
 const manifest=JSON.parse(await readFile(manifestURL,'utf8'));
 if(JSON.stringify(manifest.hashes)!==JSON.stringify(hashes)) throw Error('Arcade source or prepared media changed. Rebuild, verify the loop and ghost masks, then record its manifest.');
 console.log('Arcade After Hours sources and prepared media match the manifest.');
}
