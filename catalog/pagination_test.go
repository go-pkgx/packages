package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestListTagsFollowsPagination is the defect that cost a fifth of the
// catalogue. ghcr answers /tags/list with 100 tags and a Link header saying
// there are more; reading the first page only, astral.sh/ruff came back with
// 100 of its 207 tags and mesonbuild.com stopped at 1.11.2 while 1.12.0 sat
// published with all four platforms.
func TestListTagsFollowsPagination(t *testing.T) {
	page := 0
	d := mockDoer{fn: func(r *http.Request) (*http.Response, error) {
		page++
		switch page {
		case 1:
			h := http.Header{}
			h.Set("Link", `</v2/acme/packages/x/tags/list?last=2.0&n=1000>; rel="next"`)
			return resp(200, `{"tags":["1.0","2.0"]}`, h), nil
		case 2:
			// Absolute URL, and a relation that is not "next" alongside it.
			h := http.Header{}
			h.Set("Link", `</v2/prev>; rel="previous", <https://ghcr.test/v2/acme/packages/x/tags/list?last=4.0>; rel="next"`)
			return resp(200, `{"tags":["3.0","4.0"]}`, h), nil
		default:
			return resp(200, `{"tags":["5.0","sha256-abc","latest"]}`, nil), nil
		}
	}}
	got, err := testClient(d).listTags("x", "t")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "1.0 2.0 3.0 4.0 5.0" {
		t.Fatalf("got %v, want every page's semver tags", got)
	}
	if page != 3 {
		t.Errorf("walked %d pages, want 3", page)
	}
}

func TestListTagsStopsWithoutALink(t *testing.T) {
	calls := 0
	d := mockDoer{fn: func(*http.Request) (*http.Response, error) {
		calls++
		return resp(200, `{"tags":["1.0"]}`, nil), nil
	}}
	if _, err := testClient(d).listTags("x", "t"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("made %d requests, want 1", calls)
	}
}

// TestListTagsCannotSpinForever: a server that always claims another page must
// not hold the crawl open indefinitely.
func TestListTagsCannotSpinForever(t *testing.T) {
	calls := 0
	d := mockDoer{fn: func(*http.Request) (*http.Response, error) {
		calls++
		h := http.Header{}
		h.Set("Link", `</v2/next>; rel="next"`)
		return resp(200, `{"tags":["1.0"]}`, h), nil
	}}
	if _, err := testClient(d).listTags("x", "t"); err != nil {
		t.Fatal(err)
	}
	if calls != maxTagPages {
		t.Errorf("made %d requests, want the cap of %d", calls, maxTagPages)
	}
}

func TestNextPage(t *testing.T) {
	c := testClient(route())
	for _, tc := range []struct{ in, want string }{
		{`</v2/x?last=1>; rel="next"`, "https://ghcr.test/v2/x?last=1"},
		{`<https://elsewhere/v2/x>; rel="next"`, "https://elsewhere/v2/x"},
		{`</v2/x>; rel="previous"`, ""},
		{"", ""},
		{`malformed; rel="next"`, ""},
		{`>backwards<; rel="next"`, ""},
	} {
		if got := c.nextPage(tc.in); got != tc.want {
			t.Errorf("nextPage(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestListTagsPaginationErrors: a page that fails mid-walk fails the listing
// rather than returning a silent partial answer — which is the whole lesson.
func TestListTagsPaginationErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		code       int
	}{
		{"non-2xx on page 2", "boom", 500},
		{"undecodable page 2", "{oops", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := 0
			d := mockDoer{fn: func(*http.Request) (*http.Response, error) {
				page++
				if page == 1 {
					h := http.Header{}
					h.Set("Link", `</v2/next>; rel="next"`)
					return resp(200, `{"tags":["1.0"]}`, h), nil
				}
				return resp(tc.code, tc.body, nil), nil
			}}
			if _, err := testClient(d).listTags("x", "t"); err == nil {
				t.Fatal("a failed page must fail the listing, not truncate it")
			}
		})
	}
	// A 404 on a later page means the repository went away mid-walk: no tags,
	// no error, same as a 404 on the first.
	page := 0
	d := mockDoer{fn: func(*http.Request) (*http.Response, error) {
		page++
		if page == 1 {
			h := http.Header{}
			h.Set("Link", `</v2/next>; rel="next"`)
			return resp(200, `{"tags":["1.0"]}`, h), nil
		}
		return resp(404, "", nil), nil
	}}
	got, err := testClient(d).listTags("x", "t")
	if err != nil || got != nil {
		t.Fatalf("got %v, %v; want nothing and no error", got, err)
	}
	_ = fmt.Sprint()
}
