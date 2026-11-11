import {build} from 'esbuild';
import {readFile, writeFile} from 'node:fs/promises';
await build({entryPoints:['src/plugin.ts'],bundle:true,platform:'node',target:'node20',format:'esm',
  banner:{js:"import {createRequire} from 'node:module';const require=createRequire(import.meta.url);"},
  outfile:'com.kreuzhofer.restreamer.sdPlugin/bin/plugin.js'});
const dependencies=['@elgato/streamdeck','@elgato/utils','@elgato/schemas','ws','zod'];
const licenses=await Promise.all(dependencies.map(async name=>`${name}\n\n${await readFile(`node_modules/${name}/LICENSE`,'utf8')}`));
await writeFile('com.kreuzhofer.restreamer.sdPlugin/bin/THIRD-PARTY-LICENSES.txt',licenses.join('\n\n--------------------\n\n'));
