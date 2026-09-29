package httpapi

import (
	"encoding/xml"
	"github.com/M3264/blog-api-go/internal/storage"
	"net/http"
	"time"
)

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	Date        string `xml:"pubDate"`
}
type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Items       []rssItem `xml:"item"`
}
type rss struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}
type sitemapURL struct {
	Loc     string `xml:"loc"`
	Lastmod string `xml:"lastmod,omitempty"`
}
type sitemap struct {
	XMLName xml.Name     `xml:"urlset"`
	NS      string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

func (a *API) syndication(w http.ResponseWriter, r *http.Request) {
	ps, _, e := a.db.List(r.Context(), storage.Filter{Public: true, Limit: 10000})
	if e != nil {
		a.internal(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	if r.URL.Path == "/rss.xml" {
		feed := rss{Version: "2.0", Channel: rssChannel{Title: "Offscript", Link: a.community.Config.URL, Description: "Stories that go somewhere."}}
		for i, p := range ps {
			if i >= 50 {
				break
			}
			link := a.community.Config.URL + "/stories/" + p.Slug
			date := ""
			if p.PublishedAt != nil {
				date = p.PublishedAt.Format(time.RFC1123Z)
			}
			feed.Channel.Items = append(feed.Channel.Items, rssItem{p.Title, link, link, p.Summary, date})
		}
		xml.NewEncoder(w).Encode(feed)
	} else {
		m := sitemap{NS: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: []sitemapURL{{Loc: a.community.Config.URL + "/"}, {Loc: a.community.Config.URL + "/topics"}, {Loc: a.community.Config.URL + "/authors"}}}
		for _, p := range ps {
			m.URLs = append(m.URLs, sitemapURL{a.community.Config.URL + "/stories/" + p.Slug, p.UpdatedAt.Format(time.RFC3339)})
		}
		xml.NewEncoder(w).Encode(m)
	}
}
