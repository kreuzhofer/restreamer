import fs from 'node:fs/promises';
export async function buildPalettePlayer(root) {
  const template=await fs.readFile(new URL('player.template.html',root),'utf8');
  const art=await fs.readFile(new URL('background.png',root));
  await fs.writeFile(new URL('index.html',root),template.replace('__ARTWORK_DATA__','data:image/png;base64,'+art.toString('base64')));
  console.log('Built self-contained interactive motion study.');

}
