package actresslookup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func buildMovieBoxes(count int) string {
	items := make([]string, 0, count)
	for index := 0; index < count; index++ {
		items = append(items, fmt.Sprintf(`<a class="movie-box" href="/movie/%d">影片 %d</a>`, index+1, index+1))
	}
	return strings.Join(items, "")
}

func buildStarPageHTML(actressName string, magnetCount int, allCount int, itemsPerPage int) string {
	return fmt.Sprintf(`
		<html>
			<head><title>%s - JAVBus</title></head>
			<body>
				<div class="star-box"><span class="star-name">%s</span></div>
				<div>已有磁力 %d 部</div>
				<div>全部影片 %d 部</div>
				<div class="movies">%s</div>
			</body>
		</html>
	`, actressName, actressName, magnetCount, allCount, buildMovieBoxes(itemsPerPage))
}

func TestFetchHTMLAddsAgeVerificationCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.Contains(request.Header.Get("Cookie"), "age_verified=1") {
			t.Fatalf("expected age verification cookie, got %q", request.Header.Get("Cookie"))
		}
		_, _ = writer.Write([]byte(buildStarPageHTML("测试女优", 28, 59, 28)))
	}))
	defer server.Close()

	body, resolvedURL, err := fetchHTML(server.URL+"/star/test", "")
	if err != nil {
		t.Fatalf("fetchHTML returned error: %v", err)
	}
	if resolvedURL == "" {
		t.Fatal("expected resolvedURL to be populated")
	}
	if !strings.Contains(body, "已有磁力 28 部") {
		t.Fatalf("expected body to contain count marker, got %q", body)
	}
}

func TestFetchLookupPageHonorsCancelledContext(t *testing.T) {
	var receivedRequest atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedRequest.Store(true)
		_, _ = writer.Write([]byte(buildStarPageHTML("测试女优", 28, 59, 28)))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := NewService().fetchLookupPage(ctx, server.URL+"/star/test", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled lookup request, got %v", err)
	}
	if receivedRequest.Load() {
		t.Fatal("cancelled lookup request must not reach the provider")
	}
}

type verifiedLookupFetcherStub struct {
	called atomic.Bool
	body   string
}

func (s *verifiedLookupFetcherStub) FetchLookupHTML(ctx context.Context, targetURL string) (string, string, error) {
	s.called.Store(true)
	return s.body, targetURL, nil
}

func TestFetchLookupPageDelegatesVerificationDocumentToExistingFetcher(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("<html><title>Just a moment</title></html>"))
	}))
	defer server.Close()

	stub := &verifiedLookupFetcherStub{body: buildStarPageHTML("测试女优", 28, 59, 28)}
	body, resolvedURL, err := NewService(stub).fetchLookupPage(context.Background(), server.URL+"/star/test", "")
	if err != nil {
		t.Fatalf("fetch lookup page: %v", err)
	}
	if !stub.called.Load() {
		t.Fatal("verification document must be delegated to the existing crawler fetcher")
	}
	if resolvedURL != server.URL+"/star/test" || !strings.Contains(body, "已有磁力 28 部") {
		t.Fatalf("unexpected delegated lookup response: url=%q body=%q", resolvedURL, body)
	}
}

func TestResolveTargetReturnsProfileFromSearchResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/searchstar/三上悠亚", "/searchstar/三上悠亜":
			_, _ = writer.Write([]byte(`
				<html><body>
					<a class="avatar-box" href="/star/okq">
						<img title="三上悠亜" />
						<div class="mleft">三上悠亜</div>
					</a>
				</body></html>
			`))
		case "/star/okq":
			_, _ = writer.Write([]byte(buildStarPageHTML("三上悠亜", 194, 397, 30)))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	service := NewService()
	profile, err := service.ResolveTarget(ResolveOptions{
		ActressName:   "三上悠亜",
		PreferredBase: server.URL,
		FallbackBases: []string{server.URL},
	})
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}

	if profile.ResolvedActressName != "三上悠亜" {
		t.Fatalf("expected resolved actress name, got %+v", profile)
	}
	if profile.ResolvedBase != server.URL+"/star/okq" {
		t.Fatalf("expected resolved base to use server URL, got %s", profile.ResolvedBase)
	}
	if profile.MagnetCount != 194 || profile.AllCount != 397 {
		t.Fatalf("unexpected counts: %+v", profile)
	}
	if profile.FillCount != 194 || profile.TotalPages != 7 {
		t.Fatalf("unexpected fill count/pages: %+v", profile)
	}
}

func TestResolveTargetMatchesChineseVariantAgainstJapaneseDirectoryName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/searchstar/三上悠亚", "/searchstar/三上悠亜":
			_, _ = writer.Write([]byte(`<html><body><a class="avatar-box" href="/star/okq"><img title="三上悠亜" /></a></body></html>`))
		case "/star/okq":
			_, _ = writer.Write([]byte(buildStarPageHTML("三上悠亜", 194, 397, 30)))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	profile, err := NewService().ResolveTarget(ResolveOptions{ActressName: "三上悠亚", PreferredBase: server.URL})
	if err != nil {
		t.Fatalf("resolve Chinese variant: %v", err)
	}
	if profile.ResolvedActressName != "三上悠亜" || profile.ResolvedBase != server.URL+"/star/okq" {
		t.Fatalf("unexpected variant resolution: %+v", profile)
	}
}

func TestResolveTargetUsesBundledChineseAliasesBeforeDirectoryLookup(t *testing.T) {
	for query, canonical := range map[string]string{
		"相泽南":  "相沢みなみ",
		"高桥圣子": "高橋しょう子",
	} {
		t.Run(query, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				expectedSearchPath := "/searchstar/" + canonical
				switch request.URL.Path {
				case expectedSearchPath:
					_, _ = writer.Write([]byte(`<html><body><a class="avatar-box" href="/star/verified"><img title="` + canonical + `" /></a></body></html>`))
				case "/star/verified":
					_, _ = writer.Write([]byte(buildStarPageHTML(canonical, 28, 59, 30)))
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()

			profile, err := NewService().ResolveTarget(ResolveOptions{ActressName: query, PreferredBase: server.URL})
			if err != nil {
				t.Fatalf("resolve bundled alias: %v", err)
			}
			if profile.ResolvedActressName != canonical || profile.ResolvedBase != server.URL+"/star/verified" {
				t.Fatalf("unexpected bundled alias resolution: %+v", profile)
			}
		})
	}
}

func TestInspectTargetFallsBackAcrossOrigins(t *testing.T) {
	primary := httptest.NewServer(http.NotFoundHandler())
	defer primary.Close()

	fallback := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/star/okq" {
			_, _ = writer.Write([]byte(buildStarPageHTML("三上悠亜", 194, 397, 30)))
			return
		}
		http.NotFound(writer, request)
	}))
	defer fallback.Close()

	service := NewService()
	profile, err := service.InspectTarget(ResolveOptions{
		ActressName:   "三上悠亜",
		TargetURL:     primary.URL + "/star/okq",
		PreferredBase: fallback.URL,
		FallbackBases: []string{fallback.URL},
	})
	if err != nil {
		t.Fatalf("inspect target with fallback: %v", err)
	}

	if profile.ResolvedBase != fallback.URL+"/star/okq" {
		t.Fatalf("expected fallback origin to be used, got %s", profile.ResolvedBase)
	}
}

func TestInspectTargetFallsBackToResolveByName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/searchstar/三上悠亜":
			_, _ = writer.Write([]byte(`
				<html><body>
					<a class="avatar-box" href="/star/okq">
						<img title="三上悠亜" />
					</a>
				</body></html>
			`))
		case "/star/okq":
			_, _ = writer.Write([]byte(buildStarPageHTML("三上悠亜", 194, 397, 30)))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	service := NewService()
	profile, err := service.InspectTarget(ResolveOptions{
		ActressName:   "三上悠亜",
		TargetURL:     server.URL + "/star/missing",
		PreferredBase: server.URL,
		FallbackBases: []string{server.URL},
	})
	if err != nil {
		t.Fatalf("inspect target fallback to resolve by name: %v", err)
	}

	if profile.ResolvedBase != server.URL+"/star/okq" {
		t.Fatalf("expected resolved URL from search result, got %s", profile.ResolvedBase)
	}
}
