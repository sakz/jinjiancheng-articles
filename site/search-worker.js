let indexPromise;
async function loadIndex() {
  if (!indexPromise) {
    indexPromise = fetch('/data/search.json').then(response => {
      if (!response.ok) throw new Error(response.status);
      return response.json();
    }).then(articles => articles.map(a => ({ ...a, normalizedTitle: a.title.normalize('NFKC').toLocaleLowerCase(), normalizedText: a.text.toLocaleLowerCase(), normalizedComments: a.commentText.toLocaleLowerCase() })));
    indexPromise.catch(() => { indexPromise = undefined; });
  }
  return indexPromise;
}
self.onmessage = async ({ data }) => {
  try {
    const articles = await loadIndex();
    const matches = [];
    for (const a of articles) {
      if ((data.account && a.account !== data.account) || (data.year && a.year !== data.year)) continue;
      const combined = a.normalizedTitle + ' ' + a.normalizedText + (data.scope === 'all' ? ' ' + a.normalizedComments : '');
      if (!data.keywords.every(k => combined.includes(k))) continue;
      let snippet = a.excerpt;
      const inBody = data.keywords.map(k => a.normalizedText.indexOf(k)).filter(i => i >= 0);
      const inComments = data.keywords.map(k => a.normalizedComments.indexOf(k)).filter(i => i >= 0);
      const source = inBody.length ? a.text : (inComments.length && data.scope === 'all' ? a.commentText : '');
      const position = inBody.length ? Math.min(...inBody) : Math.min(...inComments);
      if (source) snippet = (position > 35 ? '…' : '') + source.slice(Math.max(0, position - 35), position + 115) + '…';
      matches.push({ id: a.id, snippet, score: data.keywords.filter(k => a.normalizedTitle.includes(k)).length });
    }
    matches.sort((a, b) => b.score - a.score);
    self.postMessage({ id: data.id, matches });
  } catch {
    self.postMessage({ id: data.id, error: true });
  }
};
