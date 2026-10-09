import fs from 'node:fs/promises';
import {spawn} from 'node:child_process';
import {once} from 'node:events';
import {createHash} from 'node:crypto';
import {fileURLToPath} from 'node:url';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const root=new URL('.',import.meta.url);
const preset=process.argv.includes('--subtle')?0:1;
const browser=await chromium.launch({headless:true,args:['--use-gl=angle','--use-angle=swiftshader','--enable-unsafe-swiftshader']});
try {
  const page=await browser.newPage({viewport:{width:1440,height:1080}});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(new URL('index.html?export=1',root).href);
  await page.waitForFunction(()=>window.study.ready||window.study.error);
  const failure=await page.evaluate(()=>window.study.error);if(failure)throw Error(failure);
  const image=async(time,amount=1)=> {
    await page.evaluate(([t,a,p])=>{window.study.contrast(p);window.study.render(t,a)},[time,amount,preset]);
    return Buffer.from(await page.evaluate(()=>window.study.png()),'base64');
  };
  const first=await image(0),repeat=await image(16),quarter=await image(4),still=await image(0,0);
  const hash=b=>createHash('sha256').update(b).digest('hex');
  if(hash(first)!==hash(repeat))throw Error('Loop endpoint is not exact');
  if(hash(first)===hash(quarter))throw Error('Background is not animated');
  const measurements=await page.evaluate(()=>{
    const capture=t=>{window.study.render(t);return window.study.pixels()};
    const a=capture(0),b=capture(4),last=capture(16-1/30);
    const w=1920,h=1080;
    let titleChanges=0,changed=0,delta=0,seam=0;
    for(let y=0;y<h;y++)for(let x=0;x<w;x++){
      const i=(y*w+x)*4;
      const sy=h-1-y;
      for(let k=0;k<3;k++){
        const d=Math.abs(a[i+k]-b[i+k]);delta+=d;seam+=Math.abs(a[i+k]-last[i+k]);
        if(d>0){changed++;if(x>=580&&x<1340&&sy>=210&&sy<528)titleChanges++}
      }
    }
    if(titleChanges)throw Error('Headline or subline changed');
    return {titleChangedChannels:titleChanges,changedFraction:changed/(w*h*3),meanQuarterDelta:delta/(w*h*3),meanLoopSeamDelta:seam/(w*h*3)};
  });
  const contrastComparison=await page.evaluate(()=>{
    const energy=(preset,effects)=>{
      window.study.contrast(preset);
      window.study.render(0,1,effects);const a=window.study.pixels();
      window.study.render(.37,1,effects);const b=window.study.pixels();
      let total=0;for(let i=0;i<a.length;i++)if(i%4!==3)total+=Math.abs(a[i]-b[i]);
      return total/(a.length*.75);
    };
    const result={};
    ['water','screens','lights','stars'].forEach((name,index)=>{
      const effects=[0,0,0,0];effects[index]=1;
      const subtle=energy(0,effects),vivid=energy(1,effects);
      if(vivid<=subtle)throw Error(name+' contrast did not increase');
      result[name]={subtle,vivid,ratio:vivid/subtle};
    });
    window.study.contrast(1);return result;
  });
  const report={preset:preset?'vivid':'subtle',duration:16,fps:30,width:1920,height:1080,frames:480,exactLoopEndpoint:true,...measurements,contrastComparison,pageErrors:errors};
  await fs.writeFile(new URL(preset?'validation.json':'validation-subtle.json',root),JSON.stringify(report,null,2)+'\n');
  await fs.writeFile(new URL('poster.png',root),first);
  await fs.writeFile(new URL('frame-04.png',root),quarter);
  await fs.writeFile(new URL('original-still.png',root),still);
  await page.evaluate(()=>window.study.render(0));
  await page.screenshot({path:fileURLToPath(new URL('desktop-player.png',root)),fullPage:true});
  const mobile=await browser.newPage({viewport:{width:390,height:844}});
  await mobile.goto(new URL('index.html?export=1',root).href);
  await mobile.waitForFunction(()=>window.study.ready);
  if(await mobile.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Mobile overflow');
  await mobile.screenshot({path:fileURLToPath(new URL('mobile-player.png',root)),fullPage:true});
  await mobile.close();
  console.log('Validation passed: '+JSON.stringify(report));
  if(process.argv.includes('--check'))process.exitCode=0;
  else {
    const target=fileURLToPath(new URL(preset?'arcade-after-hours-vivid-1080p.mp4':'arcade-after-hours-1080p.mp4',root));
    const ffmpeg=spawn(process.env.FFMPEG || 'ffmpeg',['-y','-v','warning','-f','image2pipe','-framerate','30','-vcodec','png','-i','pipe:0','-an','-c:v','libx264','-preset','medium','-crf','17','-pix_fmt','yuv420p','-r','30','-frames:v','480','-movflags','+faststart',target],{stdio:['pipe','ignore','inherit']});
    const completion=once(ffmpeg,'close');
    ffmpeg.stdin.on('error',()=>{});
    for(let n=0;n<480;n++){
      const frame=await image(n/30);
      if(!ffmpeg.stdin.write(frame))await once(ffmpeg.stdin,'drain');
      if(n%60===0)console.log('Rendered '+n+'/480 frames');
    }
    ffmpeg.stdin.end();
    const [exit]=await completion;if(exit!==0)throw Error('FFmpeg failed: '+exit);
    console.log('Rendered '+target);
  }
}finally{await browser.close()}
