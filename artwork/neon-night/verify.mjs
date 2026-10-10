import {createHash} from 'node:crypto';
import {readFile,writeFile} from 'node:fs/promises';
const root=new URL('../../',import.meta.url);
const manifestURL=new URL('internal/relay/artwork/neon-night/manifest.json',root);
const files=['artwork/shared/build-palette-player.mjs','artwork/shared/render-palette-loop.mjs','artwork/neon-night/background.png','artwork/neon-night/player.template.html','artwork/neon-night/build.mjs','artwork/neon-night/render.mjs','artwork/neon-night/check-masks.mjs','internal/relay/artwork/neon-night/loop.mp4','internal/relay/artwork/neon-night/poster.png','internal/mediaauthor/pixel-font.json'];
const hashes={};
for(const file of files) hashes[file]=createHash('sha256').update(await readFile(new URL(file,root))).digest('hex');
if(process.argv.includes('--record')) {
 await writeFile(manifestURL,JSON.stringify({width:1920,height:1080,fps:30,duration:16,hashes},null,2)+'\n');
} else {
 const manifest=JSON.parse(await readFile(manifestURL,'utf8'));
 if(JSON.stringify(manifest.hashes)!==JSON.stringify(hashes)) throw Error('Neon source or prepared media changed. Rebuild, verify the loop, then record its manifest.');
 console.log('Neon Night sources and prepared media match the manifest.');
}
