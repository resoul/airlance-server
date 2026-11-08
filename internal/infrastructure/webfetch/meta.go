package webfetch

import (
	"html"
	"net/url"
	"regexp"
	"strings"
)

var (
	metaTag  = regexp.MustCompile(`(?is)<meta\s[^>]*>`)
	metaAttr = regexp.MustCompile(`(?is)([a-z][a-z0-9:_-]*)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	titleTag = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

type meta struct {
	values map[string]string
	page   string
}

func parseMeta(source string) meta {
	if end := strings.Index(strings.ToLower(source), "</head>"); end > 0 {
		source = source[:end]
	}

	found := meta{values: map[string]string{}}

	for _, tag := range metaTag.FindAllString(source, -1) {
		attrs := map[string]string{}

		for _, pair := range metaAttr.FindAllStringSubmatch(tag, -1) {
			value := pair[2]
			if value == "" {
				value = pair[3]
			}

			attrs[strings.ToLower(pair[1])] = value
		}

		key := strings.ToLower(attrs["property"])
		if key == "" {
			key = strings.ToLower(attrs["name"])
		}

		content, ok := attrs["content"]
		if key != "" && ok {
			if _, taken := found.values[key]; !taken {
				found.values[key] = strings.TrimSpace(html.UnescapeString(content))
			}
		}
	}

	if match := titleTag.FindStringSubmatch(source); match != nil {
		found.page = strings.TrimSpace(html.UnescapeString(match[1]))
	}

	return found
}

func (m meta) first(keys ...string) string {
	for _, key := range keys {
		if value := m.values[key]; value != "" {
			return value
		}
	}

	return ""
}

func (m meta) title() string {
	if value := m.first("og:title", "twitter:title"); value != "" {
		return value
	}

	return m.page
}

func (m meta) summary() string {
	return m.first("og:description", "twitter:description", "description")
}

func (m meta) site(page *url.URL) string {
	if value := m.first("og:site_name"); value != "" {
		return value
	}

	return strings.TrimPrefix(strings.ToLower(page.Hostname()), "www.")
}

func (m meta) image(page *url.URL) string {
	raw := m.first("og:image", "og:image:url", "twitter:image", "twitter:image:src")
	if raw == "" {
		return ""
	}

	reference, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	resolved := page.ResolveReference(reference)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}

	return resolved.String()
}
