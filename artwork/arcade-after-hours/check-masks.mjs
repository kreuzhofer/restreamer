import fs from 'node:fs/promises';
import {fileURLToPath} from 'node:url';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const root=new URL('.',import.meta.url);
await fs.mkdir(new URL('debug/',root),{recursive:true});
const browser=await chromium.launch({headless:true,args:['--use-gl=angle','--use-angle=swiftshader','--enable-unsafe-swiftshader']});
try {
  const page=await browser.newPage({viewport:{width:1920,height:1400}});
  await page.goto(new URL('index.html?export=1',root).href);
  await page.waitForFunction(()=>window.study.ready);
  const effects=process.argv.includes('--isolate')?[[1,0,0,0],[0,1,0,0],[0,0,1,0],[0,0,0,1]]:[[1,1,1,1]];
  const results=await page.evaluate(effectSets=>{
    window.study.render(0,0);
    const original=window.study.pixels();
    const boxes=[
      {name:'left orange ghost',box:[323,569,359,609]},
      {name:'right orange ghost',box:[1167,693,1205,727]}
    ];
    const reports=[];
    for(const effects of effectSets) {
      const row={effects,ghosts:[]};
      for(const {name,box} of boxes) {
        const indices=[];
        for(let sy=box[1];sy<box[3];sy++)for(let sx=box[0];sx<box[2];sx++){
          const x=Math.floor((sx+.5)*1920/1672),y=1079-Math.floor((sy+.5)*1080/941);
          const i=(y*1920+x)*4,r=original[i],g=original[i+1],b=original[i+2];
          if(r>125&&g>60&&r>g*1.23&&g>b*1.15)indices.push(i);
        }
        if(indices.length<50)throw Error('Ghost sample is missing: '+name);
        let changed=0,maxDifference=0;
        for(const t of [0,.37,1.2,3.1]) {
          window.study.render(t,1,effects);const pixels=window.study.pixels();
          for(const i of indices){
            const d=Math.max(...[0,1,2].map(k=>Math.abs(original[i+k]-pixels[i+k])));
            if(d)changed++;maxDifference=Math.max(maxDifference,d);
          }
        }
        row.ghosts.push({name,sampledBodyPixels:indices.length,changedSamples:changed,maxDifference});
      }
      reports.push(row);
    }
    return reports;
  },effects);
  await fs.writeFile(new URL('mask-validation.json',root),JSON.stringify(results,null,2)+'\n');
  const bounds=await page.locator('#scene').boundingBox();
  for(const [label,amount] of [['original',0],['animated',1]]) {
    await page.evaluate(a=>window.study.render(.37,a),amount);
    await page.screenshot({path:fileURLToPath(new URL('debug/ghost-'+label+'.png',root)),clip:{
      x:bounds.x+1110/1672*bounds.width,y:bounds.y+662/941*bounds.height,
      width:150/1672*bounds.width,height:104/941*bounds.height
    }});
  }
  console.log(JSON.stringify(results,null,2));
  if(results.some(r=>r.ghosts.some(g=>g.changedSamples)))throw Error('Effect overlaps orange ghost artwork');
  console.log('PASS: orange ghost body pixels remain unchanged across the loop.');
}finally{await browser.close()}
