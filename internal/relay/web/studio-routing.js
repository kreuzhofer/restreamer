'use strict';
(() => {
  const collections = ['designs','music','media','themes','templates'];
  function safeBack(value) {
    if(value==='/setup')return value;
    if(!value?.startsWith('/library'))return '/setup';
    const url=new URL(value,'http://local');
    if(url.origin!=='http://local'||url.pathname!=='/library'||url.hash||[...url.searchParams.keys()].some(key=>!['collection','q'].includes(key))||url.searchParams.has('collection')&&!collections.includes(url.searchParams.get('collection'))||(url.searchParams.get('q')||'').length>160)return '/setup';
    return url.pathname+url.search;
  }
  function parse(url) {
    const params = url.searchParams;
    const design = params.get('id') || '';
    const editor = params.get('editor') === 'brb' ? 'brb' : design ? 'design' : '';
    const requested = params.get('collection');
    const collection = collections.includes(requested) ? requested : 'designs';
    let page = url.pathname.slice(1);
    if (['studio','generator','generator.html'].includes(page)) page = editor ? 'editor' : collections.includes(requested) ? 'library' : 'setup';
    if (!['live','setup','library','settings','editor'].includes(page)) page = 'live';
    const back = params.get('return');
    return {page, collection, design, editor, back:safeBack(back)};
  }
  window.studioRouting = {parse};
})();
