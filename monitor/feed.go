package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

type Article struct{ Account, Title, URL, Identity, Date, Body string }

func articleURL(raw string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() != "mp.weixin.qq.com" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Port() != "" {
		return "", "", fmt.Errorf("文章链接必须为微信原文")
	}
	u.Scheme = "https"
	u.Host = "mp.weixin.qq.com"
	u.Fragment = ""
	q := u.Query()
	if u.Path == "/s" {
		if q.Get("__biz") == "" || q.Get("mid") == "" || q.Get("idx") == "" || q.Get("sn") == "" {
			return "", "", fmt.Errorf("文章长链接缺少稳定标识，不能归档临时链接")
		}
		clean := url.Values{}
		for _, k := range []string{"__biz", "mid", "idx", "sn"} {
			clean.Set(k, q.Get(k))
		}
		u.RawQuery = clean.Encode()
		return u.String(), "long:" + q.Get("__biz") + ":" + q.Get("mid") + ":" + q.Get("idx"), nil
	}
	if !strings.HasPrefix(u.Path, "/s/") || strings.TrimPrefix(u.Path, "/s/") == "" || strings.Contains(strings.TrimPrefix(u.Path, "/s/"), "/") {
		return "", "", fmt.Errorf("不支持此微信文章链接格式")
	}
	u.RawQuery = ""
	return u.String(), "short:" + u.Path, nil
}
func readFeed(ctx context.Context, client *http.Client, s Source) ([]*gofeed.Item, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.FeedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("订阅请求创建失败")
	}
	req.Header.Set("User-Agent", "TianjiArchiveMonitor/1.0")
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("订阅源连接失败或超时")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("订阅源返回 HTTP %d", res.StatusCode)
	}
	const limit = 32 << 20
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("订阅响应读取失败")
	}
	if len(b) > limit {
		return nil, fmt.Errorf("RSS 超过 32 MiB，请减少单页数量")
	}
	feed, err := gofeed.NewParser().ParseString(string(b))
	if err != nil {
		return nil, fmt.Errorf("订阅响应不是有效 RSS/Atom/JSON Feed")
	}
	return feed.Items, nil
}
func readArticles(ctx context.Context, client *http.Client, s Source, start string) ([]*gofeed.Item, error) {
	u, _ := url.Parse(s.FeedURL)
	// Only WeRSS endpoints have the pagination contract; other feeds are consumed as published.
	paged := strings.HasPrefix(u.Path, "/feed/") || strings.HasPrefix(u.Path, "/rss/")
	var result []*gofeed.Item
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		request := s
		if paged {
			q := u.Query()
			q.Set("limit", "100")
			q.Set("offset", fmt.Sprint(page*100))
			u.RawQuery = q.Encode()
			request.FeedURL = u.String()
		}
		items, err := readFeed(ctx, client, request)
		if err != nil {
			return nil, err
		}
		old := false
		newLinks := 0
		for _, item := range items {
			date := feedDate(item)
			if start != "" && date != "" && date < start {
				old = true
				continue
			}
			if !seen[item.Link] {
				seen[item.Link] = true
				result = append(result, item)
				newLinks++
			}
		}
		if !paged || len(items) < 100 || old {
			return result, nil
		}
		if newLinks == 0 {
			return nil, fmt.Errorf("订阅源分页未前进，请检查 WeRSS 的 limit/offset 配置")
		}
	}
	return nil, fmt.Errorf("超过 100 页，请缩小 start_date 范围")
}
func itemArticle(s Source, item *gofeed.Item) (Article, error) {
	a := Article{Account: s.Account, Title: strings.Join(strings.Fields(item.Title), " ")}
	if a.Title == "" {
		return a, fmt.Errorf("文章标题为空")
	}
	var err error
	a.URL, a.Identity, err = articleURL(item.Link)
	if err != nil {
		return a, err
	}
	date := item.PublishedParsed
	if date == nil {
		date = item.UpdatedParsed
	}
	if date == nil {
		return a, fmt.Errorf("文章没有有效发布日期")
	}
	a.Date = date.In(shanghai).Format("2006-01-02")
	content := strings.TrimSpace(item.Content)
	content = strings.TrimSuffix(strings.TrimPrefix(content, "<![CDATA["), "]]>")
	if strings.TrimSpace(content) == "" {
		return a, fmt.Errorf("尚无全文，请在 WeRSS 开启正文采集和全文 RSS；稍后重试")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		return a, fmt.Errorf("HTML 解析失败")
	}
	doc.Find("script,style,iframe,noscript").Remove()
	root := doc.Find("#js_content")
	if root.Length() == 0 {
		root = doc.Find("body")
	}
	base, _ := url.Parse(a.URL)
	root.Find("img").Each(func(_ int, node *goquery.Selection) {
		if lazy, ok := node.Attr("data-src"); ok && lazy != "" {
			node.SetAttr("src", lazy)
		}
	})
	root.Find("img,a").Each(func(_ int, node *goquery.Selection) {
		attr := "href"
		if goquery.NodeName(node) == "img" {
			attr = "src"
		}
		raw, ok := node.Attr(attr)
		if !ok {
			return
		}
		u, e := url.Parse(raw)
		if e == nil && (u.Scheme == "" || u.Scheme == "http" || u.Scheme == "https") {
			node.SetAttr(attr, base.ResolveReference(u).String())
		} else {
			node.RemoveAttr(attr)
		}
	})
	html, err := root.Html()
	if err != nil {
		return a, err
	}
	if strings.TrimSpace(root.Text()) == "" && root.Find("img[src]").Length() == 0 {
		return a, fmt.Errorf("正文为空，等待数据源补抓")
	}
	converter := md.NewConverter("", true, nil)
	a.Body, err = converter.ConvertString(html)
	if err != nil {
		return a, fmt.Errorf("正文转换失败")
	}
	a.Body = strings.TrimSpace(a.Body)
	if a.Body == "" {
		return a, fmt.Errorf("正文转换后为空")
	}
	return a, nil
}
func feedDate(item *gofeed.Item) string {
	var t *time.Time = item.PublishedParsed
	if t == nil {
		t = item.UpdatedParsed
	}
	if t == nil {
		return ""
	}
	return t.In(shanghai).Format("2006-01-02")
}
