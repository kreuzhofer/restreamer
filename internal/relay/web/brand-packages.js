'use strict';
(() => {
  const panel = document.getElementById('brand-packages');
  if (!panel) return;
  const status = document.getElementById('brand-package-status');
  const list = document.getElementById('brand-package-list');
  const form = document.getElementById('brand-package-upload');
  const input = document.getElementById('brand-package-file');
  const preview = document.getElementById('brand-package-preview');
  const cancel = document.getElementById('brand-package-cancel');
  let controller, selectedFile, busy = false;
  function node(tag, text, className) {
    const element = document.createElement(tag);
    if (text) element.textContent = text;
    if (className) element.className = className;
    return element;
  }
  async function request(url, options = {}) {
    const response = await fetch(url, {credentials:'same-origin', ...options, headers:{'X-Restreamer-Control':'1', ...options.headers}});
    if (!response.ok) throw new Error((await response.text()).slice(0, 1200) || `Request failed (${response.status})`);
    return response.json();
  }
  function button(text, action, primary = false, feedback = status) {
    const element = node('button', text, primary ? 'primary-button' : 'preview-button');
    element.type = 'button';
    element.addEventListener('click', async () => {
      if (busy) return;
      element.disabled = true;
      try { feedback.textContent = ''; await action(); } catch (error) { feedback.textContent = `${error.message} Refresh packages and try again.`; }
      finally { element.disabled = false; }
    });
    return element;
  }
  function summary(pkg) {
    const section = node('section', '', 'brand-package-summary');
    section.append(node('h3', `${pkg.name} · version ${pkg.version}`));
    section.append(node('p', `${pkg.assets.length} ${pkg.assets.length === 1 ? 'asset' : 'assets'} · ${pkg.themes.length} ${pkg.themes.length === 1 ? 'theme' : 'themes'} · ${pkg.templates.length} content ${pkg.templates.length === 1 ? 'template' : 'templates'}`, 'stage-help'));
    if (pkg.notes) section.append(node('p', pkg.notes, 'stage-help'));
    const detail = node('details'); detail.append(node('summary', 'Included content'));
    const contents = node('ul');
    for (const [kind, items] of [['Asset', pkg.assets], ['Theme', pkg.themes], ['Template', pkg.templates]]) {
      for (const item of items) contents.append(node('li', `${item.name} — ${item.kind || kind}`));
    }
    detail.append(contents); section.append(detail);
    return section;
  }
  async function load() {
    try {
      const packages = await request('/api/generator/packages');
      list.replaceChildren();
      if (!packages.length) list.append(node('p', 'No brand packages installed. Import a prepared kit to add its assets, themes and editable templates together.', 'stage-help'));
      for (const pkg of packages) {
        const section = summary(pkg);
        const feedback = node('p', '', 'stage-help'); feedback.setAttribute('role', 'status');
        const packageButton = (text, action, primary = false) => button(text, action, primary, feedback);
        const actions = node('div', '', 'generator-actions');
        for (const template of pkg.templates) {
          const create = packageButton(`Create ${template.name}`, async () => {
            const name = section.querySelector('input').value.trim();
            if (!name) throw new Error('Enter a name for your show design.');
            const current = await request(`/api/generator/templates/${encodeURIComponent(template.id)}`);
            const design = await request(`/api/generator/templates/${encodeURIComponent(template.id)}/designs`, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({name,version:current.version,theme:{id:template.theme,revision:1}})});
            location.href = `/editor?id=${encodeURIComponent(design.id)}&return=${encodeURIComponent('/library?collection=packages')}`;
          }, true);
          create.className += ' studio-authoring';
          actions.append(create);
        }
        if (pkg.templates.length) {
          const label = node('label', 'New show design name', 'studio-authoring');
          const name = node('input'); name.value = `${pkg.name} show`; name.maxLength = 180;
          label.append(name); section.append(label);
          section.append(node('p', 'Creates an editable show using the current template, including any saved edits, and this package’s theme version.', 'stage-help studio-authoring'));
        }
        const themes = node('a', 'Open themes', 'context-resource-link'); themes.href = '/library?collection=themes'; actions.append(themes);
        const removal = packageButton('Review removal', async () => {
          const route = `/api/generator/packages/${encodeURIComponent(pkg.id)}/versions/${pkg.version}`;
          const plan = await request(`${route}/removal`);
          const review = node('div', '', 'brand-package-removal studio-authoring');
          review.append(node('h4', 'Remove this package?'));
          review.append(node('p', `Removes ${plan.remove.length} unused items and the package entry. Preserves ${plan.preserve.length} items that are edited or still in use.`));
          const items = node('ul');
          for (const item of plan.remove) items.append(node('li', `Remove: ${item.name} (${item.kind})`));
          for (const item of plan.preserve) items.append(node('li', `Keep: ${item.name} (${item.kind})`));
          review.append(items);
          review.append(packageButton('Remove package', async () => {
            const result = await request(route, {method:'DELETE',headers:{'Content-Type':'application/json'},body:'{}'});
            status.textContent = `Package removed. Preserved ${result.preserve.length} items still in use or edited.`;
            await load();
          }));
          review.append(packageButton('Keep package', () => review.remove()));
          section.querySelector('.brand-package-removal')?.remove();section.append(review);
        });
        removal.className += ' studio-authoring'; actions.append(removal);
        section.append(actions, feedback);list.append(section);
      }
    } catch (error) { status.textContent = `Cannot load packages: ${error.message}`; }
  }
  async function upload(file, importing) {
    if (file.size > 512 * 1024 * 1024) throw new Error('Choose a package smaller than 512 MiB.');
    busy = true; controller = new AbortController();cancel.hidden = false;
    form.querySelector('button').disabled = true;
    status.textContent = importing ? 'Importing the complete package…' : 'Uploading and validating package media. Large videos may take several minutes…';
    const data = new FormData();data.append('file', file);
    try {
      const pkg = await request(`/api/generator/packages${importing ? '' : '/preview'}`, {method:'POST',body:data,signal:controller.signal});
      preview.replaceChildren();
      if (importing || pkg.imported_at) {
        selectedFile = null;input.value = '';status.textContent = importing ? 'Package installed. Create a show below, or open Themes to explore it.' : 'This exact package version is already installed.';
        await load();
      } else {
        selectedFile = file;preview.append(summary(pkg));
        preview.append(node('p', 'Validation passed. Import adds these items without changing your existing designs or broadcast.', 'stage-help'));
        preview.append(button('Import package', () => upload(selectedFile, true), true));
        status.textContent = 'Ready to import. Review the contents below.';
      }
    } catch (error) {
      status.textContent = error.name === 'AbortError' ? 'Request cancelled. Refresh the package list to check whether an import completed.' : `Package not imported: ${error.message}`;
    } finally {busy = false;controller = null;cancel.hidden = true;form.querySelector('button').disabled = false;}
  }
  form.addEventListener('submit', event => {event.preventDefault();if (!busy && input.files[0]) upload(input.files[0], false).catch(error => {status.textContent = error.message;});});
  input.addEventListener('change', () => {selectedFile = null;preview.replaceChildren();});
  cancel.addEventListener('click', () => controller?.abort());
  document.getElementById('brand-package-refresh').addEventListener('click', load);
  window.addEventListener('studio-route-changed', () => {if (!panel.hidden && !busy) load();});
  if (!panel.hidden || new URL(location.href).searchParams.get('collection') === 'packages') load();
})();
