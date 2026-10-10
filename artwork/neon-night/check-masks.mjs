import fs from 'node:fs/promises';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const root=new URL('.',import.meta.url);
const browser=await chromium.launch({headless:true,args:['--use-gl=angle','--use-angle=swiftshader','--enable-unsafe-swiftshader']});
try {
  const page=await browser.newPage();
  await page.goto(new URL('index.html?export=1',root).href);
  await page.waitForFunction(()=>window.study.ready||window.study.error);
  const results=await page.evaluate(()=>{
    if(window.study.error)throw Error(window.study.error);
    window.study.render(0,0);
    const original=window.study.pixels(),reports=[];
    for(const [name,box] of [
      ['rooftop cat',[282,545,336,612]],
      ['sun',[1225,517,1351,640]],
      ['foreground roof',[914,808,1060,836]],
      ['water tower',[30,336,152,527]]
    ]) {
      let changed=0;
      for(const t of [0,.37,1.2,3.1,8,15.97]) {
        window.study.render(t);const pixels=window.study.pixels();
        for(let sy=box[1];sy<box[3];sy++)for(let sx=box[0];sx<box[2];sx++){
          const x=Math.floor((sx+.5)*1920/1672),y=1079-Math.floor((sy+.5)*1080/941);
          const i=(y*1920+x)*4;
          if([0,1,2].some(k=>pixels[i+k]!==original[i+k]))changed++;
        }
      }
      reports.push({name,changedSamples:changed});
    }
    return reports;
  });
  await fs.writeFile(new URL('mask-validation.json',root),JSON.stringify(results,null,2)+'\n');
  console.log(JSON.stringify(results));
  if(results.some(r=>r.changedSamples))throw Error('Effect overlaps protected Neon scenery');
}finally{await browser.close()}
