package webfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	domain "go.airlance.org/server/internal/domain/linkpreview"
	usecase "go.airlance.org/server/internal/usecase/linkpreview"
)

const (
	maxPage      = 512 << 10
	maxImage     = 3 << 20
	maxRedirects = 3
	userAgent    = "AirlanceBot/1.0 (+https://airlance.org)"
)

var (
	ErrBlocked     = errors.New("address is not allowed")
	ErrNotPage     = errors.New("not an html page")
	ErrTooLarge    = errors.New("response is too large")
	ErrBadResponse = errors.New("bad response")
)

type Client struct {
	http *http.Client
}

func New() *Client {
	return build(guard)
}

func build(control func(network, address string, conn syscall.RawConn) error) *Client {
	dialer := &net.Dialer{Timeout: 4 * time.Second, Control: control}

	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   4 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}

	return &Client{http: &http.Client{
		Transport: transport,
		Timeout:   9 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return ErrBadResponse
			}

			_, err := domain.Normalize(req.URL.String())

			return err
		},
	}}
}

func guard(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return ErrBlocked
	}

	ip := net.ParseIP(host)
	if ip == nil || blocked(ip) {
		return ErrBlocked
	}

	return nil
}

var reserved = mustNetworks(
	"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24",
	"203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "2001:db8::/32",
)

func mustNetworks(blocks ...string) []*net.IPNet {
	found := make([]*net.IPNet, len(blocks))

	for index, block := range blocks {
		_, network, err := net.ParseCIDR(block)
		if err != nil {
			panic(err)
		}

		found[index] = network
	}

	return found
}

func blocked(ip net.IP) bool {
	if mapped := ip.To4(); mapped != nil {
		ip = mapped
	}

	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return true
	}

	for _, network := range reserved {
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

func (c *Client) get(ctx context.Context, link, accept string, limit int64) ([]byte, http.Header, *url.URL, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build request: %w", err)
	}

	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", accept)
	request.Header.Set("Accept-Language", "en,uk;q=0.8,ru;q=0.6")

	response, err := c.http.Do(request)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("fetch: %w", err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, nil, nil, ErrBadResponse
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read body: %w", err)
	}

	if int64(len(body)) > limit {
		body = body[:limit]
		if limit == maxImage {
			return nil, nil, nil, ErrTooLarge
		}
	}

	return body, response.Header, response.Request.URL, nil
}

func (c *Client) Page(ctx context.Context, link string) (usecase.Page, error) {
	body, header, final, err := c.get(ctx, link, "text/html,application/xhtml+xml", maxPage)
	if err != nil {
		return usecase.Page{}, err
	}

	kind, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || (kind != "text/html" && kind != "application/xhtml+xml") {
		return usecase.Page{}, ErrNotPage
	}

	if charset := strings.ToLower(params["charset"]); charset != "" && charset != "utf-8" && charset != "us-ascii" {
		return usecase.Page{}, ErrNotPage
	}

	meta := parseMeta(string(body))
	page := usecase.Page{Site: meta.site(final), Title: meta.title(), Summary: meta.summary()}

	if image := meta.image(final); image != "" {
		page.Image, page.Width, page.Height = c.image(ctx, image)
	}

	return page, nil
}
