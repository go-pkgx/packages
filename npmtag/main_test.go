package main

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
)

// What the registry really answered for `vite` on 2026-10-23, verbatim.
const viteTags = `{"alpha":"6.0.0-alpha.24","beta":"8.3.0-beta.1","latest":"8.3.5","previous":"6.4.5"}`

func stub(t *testing.T, code int, body string, err error) *string {
	t.Helper()
	var asked string
	prev := httpGet
	httpGet = func(u string) (int, []byte, error) {
		asked = u
		return code, []byte(body), err
	}
	t.Cleanup(func() { httpGet = prev })
	return &asked
}

// The question the factory actually asks: our vite is 8.2.1, the advisory
// says `>= 8.2.0, < 8.2.4` AND `>= 8.3.0, < 8.3.3` — so "rebuild at the
// newest" is only right if the newest is past both boundaries. 8.3.5 is.
func TestItPrintsWhatTheRegistrySays(t *testing.T) {
	asked := stub(t, http.StatusOK, viteTags, nil)
	var out, errb strings.Builder
	if code := run([]string{"vite"}, &out, &errb); code != 0 {
		t.Fatalf("code=%d, want 0\n%s%s", code, out.String(), errb.String())
	}
	if *asked != "https://registry.npmjs.org/-/package/vite/dist-tags" {
		t.Errorf("it asked %q", *asked)
	}
	for _, want := range []string{"latest       8.3.5", "previous     6.4.5", "beta         8.3.0-beta.1"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q from:\n%s", want, out.String())
		}
	}
	// Sorted, so two runs of the same command are the same bytes.
	if i, j := strings.Index(out.String(), "alpha"), strings.Index(out.String(), "beta"); i > j {
		t.Errorf("the tags are not sorted:\n%s", out.String())
	}
}

func TestItCanBeAskedForOneTag(t *testing.T) {
	stub(t, http.StatusOK, viteTags, nil)
	var out, errb strings.Builder
	if code := run([]string{"vite", "latest"}, &out, &errb); code != 0 {
		t.Fatalf("code=%d, want 0\n%s%s", code, out.String(), errb.String())
	}
	if strings.Contains(out.String(), "beta") {
		t.Errorf("it printed tags nobody asked for:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "8.3.5") {
		t.Errorf("missing the version:\n%s", out.String())
	}
}

// ⛔ A TAG THAT WAS ASKED FOR AND IS ABSENT IS REPORTED, NOT SKIPPED.
// Printing nothing would read as "next is empty" rather than "there is no
// next", and the caller is deciding whether to rebuild against it.
func TestAnAbsentTagIsNamed(t *testing.T) {
	stub(t, http.StatusOK, viteTags, nil)
	var out, errb strings.Builder
	if code := run([]string{"vite", "latest", "next"}, &out, &errb); code != 0 {
		t.Fatalf("code=%d, want 0 — one of the two exists\n%s%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "next         —") {
		t.Errorf("the absent tag is not shown as absent:\n%s", out.String())
	}
	// But if NONE of them exists, that is a failed question, not an answer.
	out.Reset()
	errb.Reset()
	if code := run([]string{"vite", "next", "nope"}, &out, &errb); code != 1 {
		t.Fatalf("code=%d, want 1\n%s%s", code, out.String(), errb.String())
	}
}

// ⛔ A REGISTRY THAT WOULD NOT ANSWER MUST NOT BECOME "NO VERSIONS". Each
// of these would otherwise print an empty list, which reads as a measured
// absence and is a failed lookup.
func TestNotAnsweredIsNotAnEmptyAnswer(t *testing.T) {
	for _, c := range []struct {
		name string
		code int
		body string
		err  error
		want string
	}{
		{"no such package", http.StatusNotFound, `{"error":"Not found"}`, nil, "answered 404"},
		{"a bad day", http.StatusInternalServerError, "", nil, "answered 500"},
		{"the connection failed", 0, "", errors.New("dial tcp: connection reset by peer"), "connection reset"},
		{"not a tag map", http.StatusOK, `["vite"]`, nil, "not a tag map"},
		{"no tag at all", http.StatusOK, `{}`, nil, "named no dist-tag"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stub(t, c.code, c.body, c.err)
			var out, errb strings.Builder
			if code := run([]string{"vite"}, &out, &errb); code != 1 {
				t.Fatalf("code=%d, want 1\n%s%s", code, out.String(), errb.String())
			}
			if out.Len() != 0 {
				t.Errorf("it printed an answer it did not have: %q", out.String())
			}
			if !strings.Contains(errb.String(), c.want) {
				t.Errorf("the refusal does not say %q: %s", c.want, errb.String())
			}
		})
	}
}

// ⛔ THE NAME GOES INTO A URL PATH. A scoped package's slash is real; any
// other slash, or a `..`, is a caller reaching outside the endpoint this
// tool is for — and the request must not be made at all.
func TestTheNameCannotLeaveTheEndpoint(t *testing.T) {
	// ⛔ THE EXPECTED STRING HERE WAS WRONG FIRST, NOT THE CODE. I wrote
	// `%40scope`, because PathEscape "escapes". It does not escape `@`,
	// which is legal in a path segment — and measured against the real
	// registry on 2026-10-23, BOTH forms answer 200:
	//
	//	/-/package/@babel/core/dist-tags    200
	//	/-/package/@babel%2fcore/dist-tags  200
	//
	// So the literal slash is right, and what matters is only that no
	// segment can leave the endpoint.
	got, err := tagsURL("@scope/pkg")
	if err != nil {
		t.Fatalf("a scoped name was refused: %v", err)
	}
	if got != "https://registry.npmjs.org/-/package/@scope/pkg/dist-tags" {
		t.Errorf("a scoped name built %q", got)
	}
	for _, bad := range []string{"", "..", "a/../../etc/passwd", "a/b", "@scope/", "@scope/a/b", "./x"} {
		if u, err := tagsURL(bad); err == nil {
			t.Errorf("%q was accepted and built %q", bad, u)
		}
	}
	// And a space, which is legal in neither a package name nor a URL path.
	u, err := tagsURL("vite x")
	if err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
	if strings.Contains(u, " ") {
		t.Errorf("a raw space reached the URL: %q", u)
	}
}

func TestItRefusesToAskAboutNothing(t *testing.T) {
	stub(t, http.StatusOK, viteTags, nil)
	var out, errb strings.Builder
	if code := run(nil, &out, &errb); code != 2 {
		t.Fatalf("code=%d, want 2", code)
	}
	if !strings.Contains(errb.String(), "usage:") {
		t.Errorf("no usage: %s", errb.String())
	}
	// A name that cannot be a package is a usage error, before any request.
	errb.Reset()
	asked := stub(t, http.StatusOK, viteTags, nil)
	if code := run([]string{"a/b"}, &out, &errb); code != 2 {
		t.Fatalf("code=%d, want 2", code)
	}
	if *asked != "" {
		t.Errorf("it asked the registry anyway: %q", *asked)
	}
}

func TestMainExits(t *testing.T) {
	prevArgs, prevExit := os.Args, osExit
	code := -1
	osExit = func(c int) { code = c }
	os.Args = []string{"npmtag"}
	t.Cleanup(func() { os.Args, osExit = prevArgs, prevExit })
	main()
	if code != 2 {
		t.Errorf("no arguments exited %d, want 2", code)
	}
}
