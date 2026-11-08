package webfetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBlockedAddresses(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"127.0.0.1": true, "10.1.2.3": true, "192.168.0.9": true, "172.16.5.5": true, "169.254.169.254": true,
		"0.0.0.0": true, "100.64.1.1": true, "::1": true, "fe80::1": true, "fc00::5": true, "::ffff:10.0.0.1": true,
		"224.0.0.1": true, "8.8.8.8": false, "2606:4700:4700::1111": false,
	}

	for address, want := range cases {
		if got := blocked(net.ParseIP(address)); got != want {
			t.Errorf("%s: blocked = %v, want %v", address, got, want)
		}
	}
}

func TestTheRealClientRefusesALoopbackServer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<title>secret</title>`)
	}))
	defer server.Close()

	if _, err := New().Page(context.Background(), server.URL); err == nil {
		t.Fatal("a loopback page was fetched")
	}
}

func pictureBytes(t *testing.T, asPNG bool, width, height int) []byte {
	t.Helper()

	var out bytes.Buffer

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	if asPNG {
		if err := png.Encode(&out, canvas); err != nil {
			t.Fatal(err)
		}
	} else if err := jpeg.Encode(&out, canvas, nil); err != nil {
		t.Fatal(err)
	}

	return out.Bytes()
}

func TestAPageGivesItsCardAndImage(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/article", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><title>Plain &amp; title</title>
			<meta property="og:title" content="Заголовок &quot;статті&quot;">
			<meta property="og:description" content="Опис   на
			два рядки">
			<meta property='og:site_name' content='Новини'>
			<meta property="og:image" content="/cover.png"></head><body>ignored</body></html>`)
	})
	mux.HandleFunc("/cover.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pictureBytes(t, true, 640, 320))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	page, err := build(nil).Page(context.Background(), server.URL+"/article")
	if err != nil {
		t.Fatal(err)
	}

	if page.Title != `Заголовок "статті"` || page.Site != "Новини" || page.Summary == "" {
		t.Fatalf("got %+v", page)
	}

	if page.Width != 640 || page.Height != 320 || len(page.Image) == 0 {
		t.Fatalf("the image: %dx%d, %d bytes", page.Width, page.Height, len(page.Image))
	}

	if _, err := jpeg.Decode(bytes.NewReader(page.Image)); err != nil {
		t.Fatalf("a png must be stored as jpeg: %v", err)
	}
}

func TestThePageTitleAndHostAreTheFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title> Just a page </title></head></html>`)
	}))
	defer server.Close()

	page, err := build(nil).Page(context.Background(), server.URL)
	if err != nil || page.Title != "Just a page" || page.Site != "127.0.0.1" || len(page.Image) != 0 {
		t.Fatalf("got %+v, %v", page, err)
	}
}

func TestOnlyUTF8HTMLPagesAreRead(t *testing.T) {
	t.Parallel()

	types := []string{"application/json", "text/html; charset=windows-1251", "image/png"}
	for _, kind := range types {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", kind)
			fmt.Fprint(w, `<title>x</title>`)
		}))

		if _, err := build(nil).Page(context.Background(), server.URL); !errors.Is(err, ErrNotPage) {
			t.Errorf("%s: %v", kind, err)
		}

		server.Close()
	}
}

func TestARedirectLoopStops(t *testing.T) {
	t.Parallel()

	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+"/again", http.StatusFound)
	}))
	defer server.Close()

	if _, err := build(nil).Page(context.Background(), server.URL); err == nil {
		t.Fatal("a redirect loop was followed")
	}
}
