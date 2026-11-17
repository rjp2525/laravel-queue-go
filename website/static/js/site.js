// Small progressive enhancements: copy buttons, the mobile drawer, the
// "on this page" highlight and search. The site reads fine without any of it.
(() => {
  const $ = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

  // Copy buttons.
  $$('.code .copy').forEach((button) => {
    button.addEventListener('click', async () => {
      const code = $('pre', button.closest('.code'));
      try {
        await navigator.clipboard.writeText(code.innerText.replace(/\n$/, ''));
        button.textContent = 'copied';
        button.classList.add('is-done');
        setTimeout(() => {
          button.textContent = 'copy';
          button.classList.remove('is-done');
        }, 1600);
      } catch {
        button.textContent = 'press ctrl+c';
      }
    });
  });

  // Mobile navigation drawer.
  const workspace = $('.workspace');
  $$('[data-menu-open]').forEach((el) => el.addEventListener('click', () => workspace.classList.add('menu-is-open')));
  $$('[data-menu-close]').forEach((el) => el.addEventListener('click', () => workspace.classList.remove('menu-is-open')));

  // Keep the active sidebar entry in view.
  const active = $('.sidebar .is-active');
  if (active) active.scrollIntoView({ block: 'center' });

  // Highlight the section being read in the table of contents.
  const pane = $('[data-pane]');
  const tocLinks = $$('.toc a');
  if (pane && tocLinks.length) {
    const targets = tocLinks.map((a) => document.getElementById(decodeURIComponent(a.hash.slice(1)))).filter(Boolean);
    const update = () => {
      const edge = pane.getBoundingClientRect().top + 90;
      let current = targets[0];
      for (const el of targets) if (el.getBoundingClientRect().top <= edge) current = el;
      tocLinks.forEach((a) => a.classList.toggle('is-current', current && a.hash === '#' + current.id));
    };
    pane.addEventListener('scroll', update, { passive: true });
    update();
  }

  // Search.
  const dialog = $('[data-search]');
  const input = $('[data-search-input]');
  const results = $('[data-search-results]');
  const hint = $('[data-search-hint]');
  let index = null;
  let selected = 0;

  const escapeHtml = (s) => s.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);
  const mark = (text, terms) => {
    let out = escapeHtml(text);
    for (const term of terms) {
      out = out.replace(new RegExp('(' + term.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + ')', 'ig'), '<mark>$1</mark>');
    }
    return out;
  };

  const open = async () => {
    dialog.hidden = false;
    input.value = '';
    results.innerHTML = '';
    hint.hidden = false;
    input.focus();
    if (!index) {
      try {
        index = await (await fetch('/search.json')).json();
      } catch {
        hint.textContent = '// search is unavailable right now';
      }
    }
  };
  const close = () => { dialog.hidden = true; };

  const search = (query) => {
    const terms = query.toLowerCase().split(/\s+/).filter(Boolean);
    if (!terms.length || !index) return [];
    const hits = [];
    for (const page of index) {
      const title = page.t.toLowerCase();
      const text = page.x.toLowerCase();
      if (!terms.every((t) => title.includes(t) || text.includes(t) || page.h.some((h) => h.toLowerCase().includes(t)))) continue;

      // Prefer the most specific place the query matches: a heading, then the page.
      const heading = page.h.map((h) => h.split('|')).find(([, label]) => terms.every((t) => label.toLowerCase().includes(t)));
      let score = terms.every((t) => title.includes(t)) ? 100 : 0;
      if (heading) score += 50;
      score += Math.min(terms.reduce((n, t) => n + text.split(t).length - 1, 0), 30);

      const at = text.indexOf(terms[0]);
      const start = Math.max(0, at - 40);
      const snippet = (start > 0 ? '…' : '') + page.x.slice(start, start + 140);
      hits.push({ score, title: page.t, section: page.s, url: page.u + (heading ? '#' + heading[0] : ''), label: heading ? heading[1] : null, snippet });
    }
    return hits.sort((a, b) => b.score - a.score).slice(0, 8);
  };

  const render = () => {
    const terms = input.value.toLowerCase().split(/\s+/).filter(Boolean);
    const hits = search(input.value);
    selected = 0;
    hint.hidden = hits.length > 0;
    hint.textContent = input.value.trim() ? '// nothing matches that' : '// type to search · ↑↓ to move · enter to open';
    results.innerHTML = hits.map((hit, i) =>
      `<li><a href="${hit.url}"${i === 0 ? ' class="is-selected"' : ''}><i>${escapeHtml(hit.section)}</i><b>${mark(hit.title, terms)}${hit.label ? ' <span style="opacity:.6">›</span> ' + mark(hit.label, terms) : ''}</b><small>${mark(hit.snippet, terms)}</small></a></li>`
    ).join('');
  };

  const move = (delta) => {
    const links = $$('a', results);
    if (!links.length) return;
    selected = (selected + delta + links.length) % links.length;
    links.forEach((a, i) => a.classList.toggle('is-selected', i === selected));
    links[selected].scrollIntoView({ block: 'nearest' });
  };

  if (dialog) {
    $$('[data-search-open]').forEach((el) => el.addEventListener('click', open));
    input.addEventListener('input', render);
    dialog.addEventListener('click', (e) => { if (e.target === dialog) close(); });
    document.addEventListener('keydown', (e) => {
      const typing = /^(INPUT|TEXTAREA)$/.test(document.activeElement.tagName);
      if (dialog.hidden) {
        if ((e.key === '/' && !typing) || (e.key === 'k' && (e.metaKey || e.ctrlKey))) { e.preventDefault(); open(); }
        return;
      }
      if (e.key === 'Escape') close();
      else if (e.key === 'ArrowDown') { e.preventDefault(); move(1); }
      else if (e.key === 'ArrowUp') { e.preventDefault(); move(-1); }
      else if (e.key === 'Enter') { const a = $('a.is-selected', results); if (a) window.location.href = a.href; }
    });
  }
})();
