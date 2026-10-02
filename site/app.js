const escapeText = value => String(value).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const normalize = value => value.normalize('NFKC').toLocaleLowerCase();
const form = document.querySelector('#search-form');

if (form) {
  const query = document.querySelector('#query');
  const account = document.querySelector('#account');
  const year = document.querySelector('#year');
  const scope = document.querySelector('#scope');
  const results = document.querySelector('#results');
  const status = document.querySelector('#search-status');
  const pagination = document.querySelector('#pagination');
  let catalog = [];
  let ready = false;
  let matches = [];
  let page = 1;
  let request = 0;
  let worker;
  let debounce;
  const pageSize = 24;
  const params = new URLSearchParams(location.search);
  for (const [element, name] of [[query, 'q'], [account, 'account'], [year, 'year'], [scope, 'scope']]) {
    if (params.has(name)) element.value = params.get(name);
  }
  if (!scope.value) scope.value = 'body';

  function highlight(text) {
    const tokens = query.value.trim().split(/\s+/).filter(Boolean);
    if (!tokens.length) return escapeText(text);
    const pattern = new RegExp(tokens.map(t => t.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|'), 'giu');
    let output = '', offset = 0;
    for (const match of text.matchAll(pattern)) {
      output += escapeText(text.slice(offset, match.index)) + `<mark>${escapeText(match[0])}</mark>`;
      offset = match.index + match[0].length;
    }
    return output + escapeText(text.slice(offset));
  }

  function render() {
    const pages = Math.ceil(matches.length / pageSize);
    results.innerHTML = matches.slice((page - 1) * pageSize, page * pageSize).map(item => {
      const a = item.article;
      return `<a class="article-card" href="${a.url}"><div class="card-meta"><span>${escapeText(a.account)}</span><time>${a.date}</time></div><h3>${highlight(a.title)}</h3><p>${highlight(item.snippet || a.excerpt)}</p><span class="card-foot">约 ${a.minutes} 分钟阅读 <span aria-hidden="true">↗</span></span></a>`;
    }).join('') || '<div class="empty-state"><span>⌕</span><h2>暂时没有匹配的文章。</h2><p>试试更短的关键词，或放宽公众号、年份与检索范围。</p></div>';
    pagination.innerHTML = pages > 1 ? `<button data-page="${page - 1}"${page === 1 ? ' disabled' : ''}>← 上一页</button><span>第 ${page} / ${pages} 页</span><button data-page="${page + 1}"${page === pages ? ' disabled' : ''}>下一页 →</button>` : '';
    status.textContent = query.value.trim() ? `找到 ${matches.length} 篇匹配文章 · ${scope.selectedOptions[0].textContent}` : `共 ${matches.length} 篇 · 按发布日期倒序排列`;
    results.removeAttribute('aria-busy');
  }

  function search() {
    if (!ready) return;
    const id = ++request;
    page = 1;
    const keywords = normalize(query.value.trim()).split(/\s+/).filter(Boolean);
    const newParams = new URLSearchParams();
    for (const [key, value] of [['q', query.value.trim()], ['account', account.value], ['year', year.value], ['scope', scope.value]]) {
      if (value && (key !== 'scope' || value !== 'body')) newParams.set(key, value);
    }
    history.replaceState(null, '', location.pathname + (newParams.size ? `?${newParams}` : ''));
    if (!keywords.length || scope.value === 'title') {
      matches = catalog.filter(a => (!account.value || a.account === account.value) && (!year.value || a.year === year.value) && keywords.every(k => normalize(a.title).includes(k))).map(article => ({ article }));
      render();
      return;
    }
    status.textContent = '正在检索全文…首次检索需要载入完整正文索引。';
    results.setAttribute('aria-busy', 'true');
    if (!worker) {
      worker = new Worker('/search-worker.js');
      worker.onmessage = ({ data }) => {
        if (data.id !== request) return;
        if (data.error) {
          status.textContent = '全文索引加载失败，请检查网络后重新搜索。';
          results.removeAttribute('aria-busy');
          return;
        }
        const byId = new Map(catalog.map(a => [a.id, a]));
        matches = data.matches.map(m => ({ article: byId.get(m.id), snippet: m.snippet }));
        render();
      };
      worker.onerror = () => {
        status.textContent = '浏览器未能启动全文检索，请重试或选择“仅标题”。';
        results.removeAttribute('aria-busy');
      };
    }
    worker.postMessage({ id, keywords, account: account.value, year: year.value, scope: scope.value });
  }

  form.addEventListener('submit', event => { event.preventDefault(); clearTimeout(debounce); search(); });
  query.addEventListener('input', () => { clearTimeout(debounce); debounce = setTimeout(search, 300); });
  for (const filter of [account, year, scope]) filter.addEventListener('change', search);
  document.querySelector('#reset').addEventListener('click', () => { query.value = ''; account.value = ''; year.value = ''; scope.value = 'body'; clearTimeout(debounce); search(); query.focus(); });
  pagination.addEventListener('click', event => {
    const button = event.target.closest('button[data-page]');
    if (!button || button.disabled) return;
    page = Number(button.dataset.page); render();
    document.querySelector('.search-panel').scrollIntoView({ behavior: 'smooth', block: 'start' });
  });
  form.querySelector('button').disabled = true;
  fetch('/data/catalog.json').then(response => { if (!response.ok) throw new Error(response.status); return response.json(); })
    .then(data => { catalog = data.articles; ready = true; form.querySelector('button').disabled = false; search(); })
    .catch(() => { status.textContent = '目录加载失败，请刷新页面。现有文章链接仍可阅读。'; });
}
