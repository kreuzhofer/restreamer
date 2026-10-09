// Run against a disposable authenticated localhost server with BRB prepared.
// GENERATOR_URL=http://127.0.0.1:18805 PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node tests/generator-activation-browser.mjs
// Uses the visible browser UI and public HTTP APIs; creates designs and rehearses only.
import assert from 'node:assert/strict';
import {mkdir} from 'node:fs/promises';
const {chromium} = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.GENERATOR_URL || 'http://127.0.0.1:18805';
assert(['localhost', '127.0.0.1', '[::1]'].includes(new URL(base).hostname), 'Use a disposable local fixture');
const output = process.env.GENERATOR_BROWSER_OUTPUT || '/tmp/restreamer-activation-browser';
await mkdir(output, {recursive:true});
const browser = await chromium.launch({headless:true});
const context = await browser.newContext({httpCredentials:{username:process.env.GENERATOR_USER || 'stage-test',password:process.env.GENERATOR_PASSWORD || 'stage-test'},viewport:{width:1440,height:1050}});
const errors = [];
context.on('page', page => page.on('pageerror', error => errors.push(error.message)));
const api = async (path, method='GET', data) => {
  const response = await context.request.fetch(base+path,{method,data,headers:{'X-Restreamer-Control':'1'}});
  assert(response.ok(), `${method} ${path}: ${response.status()} ${await response.text()}`);
  return response.status() === 204 ? null : response.json();
};
async function eventually(check) {for(let n=0;n<400;n++){const result=await check();if(result)return result;await new Promise(resolve=>setTimeout(resolve,100));}throw new Error('Timed out');}
const stage = () => api('/api/stage');
const command = async (action, extra={}) => {const s=await stage();return api('/api/stage/commands','POST',{id:crypto.randomUUID(),server_id:s.server_id,context:s.context,confirmed:true,action,...extra});};
async function generate(name, kind='prestream', seconds=3) {
  let design=await api('/api/generator/designs','POST',{name,stage:kind});
  design.scenes[0].text=name;design.scenes[0].duration_seconds=seconds;
  design=await api('/api/generator/designs/'+design.id,'PUT',design);
  const submitted=await api('/api/generator/jobs','POST',{design_id:design.id,version:design.version});
  const job=await eventually(async()=>{const j=await api('/api/generator/jobs/'+submitted.id);return !['queued','running','cancelling'].includes(j.state)&&j;});
  assert.equal(job.state,'ready',job.error);return {design,job,revision:job.media_revision};
}
try {
  await command('stop_now');
  const a=await generate('Activation original'), b=await generate('Activation correction');
  const initial={prestream:a.revision,ending:a.revision,shortcuts:[{name:'Original clip',revision:a.revision}]};
  await api('/api/stage-media','PUT',initial);
  const editor=await context.newPage();await editor.goto(base+'/generator?id='+b.design.id);
  const row=editor.locator(`[data-job-id="${b.job.id}"]`);
  const link=row.getByRole('link',{name:'Select for next PRESTREAM'});
  await link.waitFor(); // Red: ready results have no activation path before #31.
  const pages=context.waitForEvent('page');await link.click();const page=await pages;
  await page.waitForLoadState('domcontentloaded');
  await page.locator('#media-intent').waitFor({state:'visible'});
  assert.equal(await page.locator('#library-revision').inputValue(),b.revision);
  assert.deepEqual(await api('/api/stage-media'),initial);
  assert.equal((await stage()).stage,'OFF');
  assert.equal(await page.locator('#library-prepare').isDisabled(),true);
  await page.locator('#media-intent-review').click();
  assert.equal(await page.locator('#selection-prestream').inputValue(),b.revision);
  assert.deepEqual(await api('/api/stage-media'),initial,'Opening and staging are not saves');
  await page.locator('#selection-save').click();
  await eventually(async()=> (await api('/api/stage-media')).prestream===b.revision);
  assert.equal((await stage()).stage,'OFF');
  assert.equal((await api('/api/stage-media')).ending,a.revision);
  assert.equal(await editor.locator('#design-name').inputValue(),'Activation correction');
  await api('/api/stage-media','PUT',initial);
  await command('prestream',{mode:'preview_only'});
  const replace=row.getByRole('link',{name:'Review Replace PRESTREAM now'});
  await replace.waitFor(); // Red until the reviewed activation bridge is implemented.
  const replacementURL=await replace.getAttribute('href');
  const control=await context.newPage(), mutations=[];
  control.on('request', request=>{if(request.method()!=='GET')mutations.push(request.url());});
  await control.goto(base+replacementURL);
  await control.locator('#media-intent').waitFor({state:'visible'});
  assert.equal(await control.locator('#media-intent-review').isDisabled(),true);
  await control.locator('#library-preview').click();
  await control.waitForFunction(()=>document.querySelector('#candidate-video').readyState>=2);
  assert.deepEqual(mutations,[],'Opening and previewing must not mutate settings or stage');
  await control.locator('#media-intent-review').click();
  await control.locator('#stage-dialog').waitFor({state:'visible'});
  assert((await control.locator('#stage-dialog-effect').textContent()).includes('Preview only'));
  assert((await control.locator('#stage-dialog-context').textContent()).includes(b.revision));
  await control.locator('#stage-dialog').screenshot({path:output+'/desktop-prestream-review.png'});
  await eventually(async()=> (await stage()).playback.position>0.4);
  assert.equal(await control.locator('#stage-dialog-confirm').isDisabled(),false,'Ordinary progress must retain review');
  await control.locator('#stage-dialog-confirm').click();
  await eventually(async()=> (await stage()).media.revision===b.revision);
  assert.equal((await stage()).playback.loop,true);
  assert.equal((await stage()).mode,'preview_only');
  assert.equal((await api('/api/stage-media')).prestream,a.revision);
  // The original URL cannot be reinterpreted after its context changes.
  await control.goto(base+replacementURL);
  await control.locator('#media-intent').waitFor({state:'visible'});
  await eventually(async()=> (await control.locator('#media-intent-status').textContent()).includes('changed'));
  assert.equal(await control.locator('#library-revision').inputValue(),b.revision);
  assert.equal(await control.locator('#media-intent-review').isDisabled(),true);
  assert.equal(await control.locator('#library-replace-now').isDisabled(),true);
  // Replacing a suspended PRESTREAM must preserve the playing clip.
  await command('stop_now');await command('prestream',{mode:'preview_only'});
  await command('play_clip',{revision:a.revision,loop:true});
  const returnLink=row.getByRole('link',{name:'Review Replace PRESTREAM on return'});await returnLink.waitFor();
  await control.goto(base+await returnLink.getAttribute('href'));
  await control.locator('#library-preview').click();await control.waitForFunction(()=>document.querySelector('#candidate-video').readyState>=2);
  const before=await stage();await control.locator('#media-intent-review').click();await control.locator('#stage-dialog-confirm').click();
  await eventually(async()=> (await stage()).return_media?.revision===b.revision);
  const after=await stage();assert.equal(after.media.revision,a.revision);assert.equal(after.playback.epoch,before.playback.epoch);assert.equal(after.return_stage,'PRESTREAM');
  await command('stop_clip');assert.equal((await stage()).media.revision,b.revision);
  // Selection changes invalidate an already-open review.
  const aRow=editor.locator(`[data-job-id="${a.job.id}"]`);
  const aLink=aRow.getByRole('link',{name:'Review Replace PRESTREAM now'});await aLink.waitFor();
  await control.goto(base+await aLink.getAttribute('href'));
  await control.locator('#library-preview').click();await control.locator('#media-intent-review').click();
  await api('/api/stage-media','PUT',{...initial,prestream:b.revision});
  await eventually(async()=> await control.locator('#stage-dialog-confirm').isDisabled());
  await control.locator('#stage-dialog-cancel').click();
  // Wrong-kind and invalid inputs retain the exact candidate without fallback.
  await command('end_stream');await control.goto(base+replacementURL);
  await control.locator('#media-intent').waitFor({state:'visible'});
  assert.equal(await control.locator('#media-intent-review').isDisabled(),true);
  assert.equal(await control.locator('#library-replace-now').isDisabled(),true);
  for(const query of [new URLSearchParams({media_revision:'not-a-revision',media_stage:'prestream',media_action:'select_next'}),new URLSearchParams({media_revision:'f'.repeat(64),media_stage:'prestream',media_action:'select_next'}),new URLSearchParams({media_revision:b.revision,media_stage:'prestream',media_action:'unsupported'})]) {
    await control.goto(base+'/?'+query);await control.locator('#media-intent').waitFor({state:'visible'});
    assert.equal(await control.locator('#media-intent-review').isDisabled(),true);
    assert.equal(await control.locator('#library-revision').inputValue(),query.get('media_revision'));
  }
  await command('stop_now');
  for (const [width,height,label] of [[1440,1050,'desktop'],[390,844,'mobile']]) {
    await editor.setViewportSize({width,height});
    await row.scrollIntoViewIfNeeded();await editor.screenshot({path:output+'/'+label+'-generator.png',fullPage:true});
    await control.setViewportSize({width,height});await control.goto(base+await link.getAttribute('href'));
    await control.locator('#library-preview').click();await control.waitForFunction(()=>document.querySelector('#candidate-video').readyState>=2);
    assert(await control.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'Dashboard overflows viewport');
    assert(await editor.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'Generator overflows viewport');
    await control.screenshot({path:output+'/'+label+'-candidate.png',fullPage:true});
    if (width === 390) {
      await control.locator('#media-intent-review').click();await control.locator('#selection-save').click();
      await eventually(async()=> (await api('/api/stage-media')).prestream===b.revision);
      await command('prestream',{mode:'preview_only'});
      const observed=await stage();
      const mobileURL=await eventually(async()=>{const href=await aLink.getAttribute('href').catch(()=>null);return href&&new URL(href,base).searchParams.get('context')===observed.context&&href;});
      await control.goto(base+mobileURL);await control.locator('#library-preview').click();
      await control.waitForFunction(()=>document.querySelector('#candidate-video').readyState>=2);
      await control.locator('#media-intent-review').click();
      await control.locator('#stage-dialog').screenshot({path:output+'/mobile-prestream-review.png'});
      await control.locator('#stage-dialog-confirm').click();await eventually(async()=> (await stage()).media.revision===a.revision);
    }
  }
  await command('stop_now');
  const endingA=await generate('Ending original','ending',8), endingB=await generate('Ending correction','ending',4);
  const endingEditor=await context.newPage();await endingEditor.goto(base+'/generator?id='+endingB.design.id);
  const endingRow=endingEditor.locator(`[data-job-id="${endingB.job.id}"]`);
  const endingSelection=endingRow.getByRole('link',{name:'Select for next ENDING'});
  await endingSelection.waitFor(); // Red before #32: ENDING has no bridge action.
  for(const [width,height,label] of [[1440,1050,'desktop'],[390,844,'mobile']]) {
    await command('stop_now');await api('/api/stage-media','PUT',{...initial,ending:endingA.revision});
    await command('prestream',{mode:'preview_only'});await command('end_stream');
    await control.setViewportSize({width,height});await endingEditor.setViewportSize({width,height});
    await control.goto(base+await endingSelection.getAttribute('href'));
    await control.locator('#media-intent').waitFor({state:'visible'});
    assert.equal((await stage()).media.revision,endingA.revision);
    await control.locator('#media-intent-review').click();
    assert.equal(await control.locator('#selection-ending').inputValue(),endingB.revision);
    await control.locator('#selection-save').click();
    await eventually(async()=> (await api('/api/stage-media')).ending===endingB.revision);
    assert.equal((await stage()).media.revision,endingA.revision,'Next selection replaced the running ending');
    const observed=await stage(), replaceEnding=endingRow.getByRole('link',{name:'Review Replace ENDING now'});
    const href=await eventually(async()=>{const href=await replaceEnding.getAttribute('href').catch(()=>null);return href&&new URL(href,base).searchParams.get('context')===observed.context&&href;});
    await control.goto(base+href);await control.locator('#library-preview').click();
    await control.waitForFunction(()=>document.querySelector('#candidate-video').readyState>=2);
    await control.locator('#media-intent-review').click();
    assert((await control.locator('#stage-dialog-effect').textContent()).includes('postpones shutdown'));
    assert((await control.locator('#stage-dialog-context').textContent()).includes(endingB.revision));
    await control.locator('#stage-dialog').screenshot({path:output+'/'+label+'-ending-review.png'});
    if(label==='desktop') await eventually(async()=> (await stage()).playback.position>5.8);
    await control.locator('#stage-dialog-confirm').click();await eventually(async()=> (await stage()).media.revision===endingB.revision);
    assert.equal((await stage()).playback.loop,false);assert.equal((await stage()).mode,'preview_only');
    if(label==='desktop') {
      await eventually(async()=> (await stage()).playback.position>2.4);
      assert.equal((await stage()).stage,'ENDING','Original EOF stopped the replacement');
    }
    await eventually(async()=> (await stage()).stage==='OFF');assert.equal((await stage()).ending.completed,true);
    await control.goto(base+href);await control.locator('#media-intent').waitFor({state:'visible'});
    assert.equal(await control.locator('#media-intent-review').isDisabled(),true);
    assert.equal(await control.locator('#library-revision').inputValue(),endingB.revision);
    await eventually(async()=> (await control.locator('#media-intent-status').textContent()).includes('ending'));
    assert.equal(await control.locator('#library-replace-now').isDisabled(),true);
    assert(await control.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
    await control.screenshot({path:output+'/'+label+'-completed-ending.png',fullPage:true});
  }
  // Completion while a confirmation is open disables it and keeps the candidate.
  await api('/api/stage-media','PUT',{...initial,ending:endingB.revision});
  await command('prestream',{mode:'preview_only'});await command('end_stream');
  const endingARow=endingEditor.locator(`[data-job-id="${endingA.job.id}"]`), endingALink=endingARow.getByRole('link',{name:'Review Replace ENDING now'});
  const endingState=await stage();
  const heldURL=await eventually(async()=>{const href=await endingALink.getAttribute('href').catch(()=>null);return href&&new URL(href,base).searchParams.get('context')===endingState.context&&href;});
  await control.goto(base+heldURL);await control.locator('#library-preview').click();await control.locator('#media-intent-review').click();
  await eventually(async()=> (await stage()).stage==='OFF');
  await eventually(async()=> await control.locator('#stage-dialog-confirm').isDisabled());
  await control.locator('#stage-dialog-cancel').click();
  assert.equal(await control.locator('#library-revision').inputValue(),endingA.revision);
  // A forged ENDING-on-return request is unsupported, even with a ready revision.
  await control.goto(base+'/?'+new URLSearchParams({media_revision:endingA.revision,media_stage:'ending',media_action:'replace_on_return',server_id:endingState.server_id,context:endingState.context}));
  await control.locator('#media-intent').waitFor({state:'visible'});
  assert.equal(await control.locator('#media-intent-review').isDisabled(),true);
  // Completed ending results remain observable during a later session. They
  // must not hide PRESTREAM controls or mislabel its current stage.
  await command('prestream',{mode:'preview_only'});
  const nextSession=await stage();
  await eventually(async()=>{const href=await replace.getAttribute('href').catch(()=>null);return href&&new URL(href,base).searchParams.get('context')===nextSession.context;});
  assert((await editor.locator('#generation-stage-status').textContent()).startsWith('PRESTREAM'));
  assert.deepEqual(errors,[]);
  console.log('PASS passive exact selection, PRESTREAM replacement/return, ENDING postponement/completion races, stale/invalid links, rehearsal, desktop/mobile, editor preserved');
} finally {await command('stop_now').catch(()=>{});await browser.close();}
