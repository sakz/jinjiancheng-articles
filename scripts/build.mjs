import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import MarkdownIt from 'markdown-it';
import { topics } from '../content/insights.mjs';
import { layout, home, archive, articlePage, insightPage, aboutPage } from '../site/templates.mjs';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const sourceRoot = path.join(root, 'md/天玑全集');
const output = path.join(root, 'dist');
const accounts = ['金渐成', '天机奇谈', '生玑伯伯'];
const md = new MarkdownIt({ html: false, linkify: true, typographer: false });
const imageRule = md.renderer.rules.image;
md.renderer.rules.image = (tokens, index, options, env, self) => {
  tokens[index].attrSet('loading', 'lazy');
  tokens[index].attrSet('decoding', 'async');
  tokens[index].attrSet('referrerpolicy', 'no-referrer');
  return imageRule(tokens, index, options, env, self);
};
md.renderer.rules.link_open = (tokens, index, options, env, self) => {
  tokens[index].attrSet('target', '_blank');
  tokens[index].attrSet('rel', 'noopener noreferrer');
  return self.renderToken(tokens, index, options);
};

function plain(text) {
  return text.replace(/<!--[^]*?-->/g, '')
    .replace(/!\[[^\]]*\]\([^\n]*?\)/g, ' ')
    .replace(/\[([^\]]+)\]\([^\n]*?\)/g, '$1')
    .replace(/https?:\/\/\S+/g, ' ')
    .replace(/\\([\\`*_{}\[\]()#+.!-])/g, '$1')
    .replace(/^[#>]+\s*/gm, '')
    .replace(/[*_`]/g, '')
    .normalize('NFKC').replace(/\s+/g, ' ').trim();
}

const articles = [];
for (const account of accounts) {
  const folder = path.join(sourceRoot, account);
  for (const name of (await fs.readdir(folder)).filter(name => name.endsWith('.md')).sort()) {
    const raw = await fs.readFile(path.join(folder, name), 'utf8');
    const date = name.match(/^\[(\d{4}-\d{2}-\d{2})\]/)?.[1];
    if (!date) throw new Error(`文章发布日期缺失: ${account}/${name}`);
    const title = raw.match(/^#\s+(.+)$/m)?.[1]?.trim() || name.replace(/^\[[^\]]+\]/, '').replace(/\.md$/, '').replace(/_/g, ' ');
    const sourceUrl = raw.match(/原文地址[^\n]*?\]\((https?:\/\/[^)]+)\)/)?.[1] || '';
    const withoutHeader = raw.replace(/^#\s+[^\n]+\n?/, '')
      .replace(/^原创[^\n]*\n?/m, '').replace(/^>\s*原文地址[^\n]*\n?/m, '').trim();
    const commentMatch = /^\s*(?:#{1,6}\s*)?(?:留言\s+(?:\d+|undefined)|留言区|最终评论\s+\d+|未检测到评论消失)\s*$/m.exec(withoutHeader);
    const body = commentMatch ? withoutHeader.slice(0, commentMatch.index).trim() : withoutHeader;
    const comments = commentMatch ? withoutHeader.slice(commentMatch.index).trim() : '';
    const id = createHash('sha256').update(`${account}/${name}`).digest('hex').slice(0, 16);
    const text = plain(body);
    articles.push({ id, account, date, year: date.slice(0, 4), title, sourceUrl,
      url: `/articles/${id}/`, excerpt: text.slice(0, 130), minutes: Math.max(1, Math.ceil(text.length / 450)),
      text, commentText: plain(comments), body, comments, sourcePath: `md/天玑全集/${account}/${name}` });
  }
}
articles.sort((a, b) => b.date.localeCompare(a.date) || a.title.localeCompare(b.title, 'zh-CN'));
if (new Set(articles.map(article => article.id)).size !== articles.length) throw new Error('文章 ID 重复');

const catalog = articles.map(({ id, account, date, year, title, sourceUrl, url, excerpt, minutes }) =>
  ({ id, account, date, year, title, sourceUrl, url, excerpt, minutes }));
const stats = { total: articles.length, accounts: accounts.map(name => ({ name, count: articles.filter(a => a.account === name).length })),
  years: [...new Set(articles.map(a => a.year))].sort().reverse(), start: articles.at(-1).date, end: articles[0].date };
const resolveReference = ref => {
  const matches = articles.filter(a => a.account === (ref.account || '金渐成') && (!ref.date || a.date === ref.date) &&
    (!ref.title || a.title.normalize('NFKC').includes(ref.title.normalize('NFKC'))));
  if (matches.length !== 1) throw new Error(`理念出处不能唯一匹配: ${JSON.stringify(ref)} (${matches.length})`);
  return matches[0];
};
for (const topic of topics) {
  for (const section of topic.sections) section.sources = section.references.map(resolveReference);
}

await fs.rm(output, { recursive: true, force: true });
await fs.mkdir(path.join(output, 'data'), { recursive: true });
await fs.cp(path.join(root, 'site/assets'), path.join(output, 'assets'), { recursive: true });
for (const filename of ['styles.css', 'app.js', 'search-worker.js']) await fs.copyFile(path.join(root, 'site', filename), path.join(output, filename));
await fs.writeFile(path.join(output, 'data/catalog.json'), JSON.stringify({ stats, articles: catalog }));
await fs.writeFile(path.join(output, 'data/search.json'), JSON.stringify(articles.map(({ id, account, date, year, title, url, excerpt, text, commentText }) =>
  ({ id, account, date, year, title, url, excerpt, text, commentText }))));

async function writePage(relativePath, title, description, content, active = '') {
  const filename = path.join(output, relativePath, 'index.html');
  await fs.mkdir(path.dirname(filename), { recursive: true });
  await fs.writeFile(filename, layout({ title, description, content, active }));
}
await writePage('', '天玑全集 · 一个人的成长，几个时代的回声', '从投资、育儿到人生与商业，阅读天玑全集的文章与理念。', home(stats, catalog, topics), 'home');
await writePage('archive', '文章档案 · 天玑全集', `${stats.total} 篇归档文章，支持标题、正文与留言关键词检索。`, archive(stats, catalog), 'archive');
for (const topic of topics) await writePage(`insights/${topic.slug}`, `${topic.title} · 天玑全集`, topic.description, insightPage(topic), topic.slug);
for (const item of articles) {
  const bodyHtml = md.render(item.body);
  const commentHtml = item.comments ? md.render(item.comments) : '';
  const nearby = articles.filter(a => a.account === item.account);
  const position = nearby.findIndex(a => a.id === item.id);
  await writePage(`articles/${item.id}`, `${item.title} · ${item.account}`, item.excerpt,
    articlePage(item, bodyHtml, commentHtml, nearby[position + 1], nearby[position - 1]), 'archive');
}
await writePage('about', '关于这份阅读档案 · 天玑全集', '收录范围、总结方法与原文回查说明。', aboutPage(stats), 'about');
await fs.writeFile(path.join(output, '404.html'), layout({ title: '页面未找到 · 天玑全集', description: '', content: '<main id="main" class="page narrow"><p class="eyebrow">404</p><h1>这页暂时不在档案里。</h1><a class="button" href="/archive/">回到文章档案 →</a></main>' }));
await fs.writeFile(path.join(output, 'data/coverage.json'), JSON.stringify({ stats, sourceFiles: articles.map(a => a.sourcePath), topics: topics.map(t => ({ slug: t.slug, sections: t.sections.length, references: t.sections.flatMap(s => s.sources.map(a => a.sourcePath)) })) }, null, 2));
console.log(`已生成 ${articles.length} 篇文章，${topics.length} 个理念栏目，${topics.reduce((n, t) => n + t.sections.length, 0)} 项理念详解。`);
for (const item of stats.accounts) console.log(`${item.name}: ${item.count} 篇`);
