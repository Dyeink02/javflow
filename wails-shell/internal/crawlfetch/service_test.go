package crawlfetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchIndexPageBuildsURLAndParsesLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/star/okq/2" {
			t.Fatalf("unexpected request path: %s", request.URL.Path)
		}
		_, _ = writer.Write([]byte(`
			<html><body>
				<a class="movie-box" href="https://www.javbus.com/ABF-055"></a>
				<a class="movie-box" href="https://www.javbus.com/ABW-006"></a>
			</body></html>
		`))
	}))
	defer server.Close()

	service, err := NewService(ServiceOptions{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := service.FetchIndexPage(context.Background(), IndexPageOptions{
		BaseURL:    server.URL + "/star/okq",
		PageNumber: 2,
	})
	if err != nil {
		t.Fatalf("fetch index page: %v", err)
	}
	if len(result.Links) != 2 {
		t.Fatalf("unexpected links: %#v", result)
	}
}

func TestFetchDetailParsesMetadataAndFilmData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`
			<html>
			  <head><meta property="og:title" content="ABF-055 Sample" /></head>
			  <body>
			    <script>var gid = 12345; var uc = 7; var img = 'https:\/\/img.example.com\/abf055.jpg';</script>
			    <span class="genre"><label><a>剧情</a></label></span>
			    <div class="star-name"><a>三上悠亜</a></div>
			  </body>
			</html>
		`))
	}))
	defer server.Close()

	service, err := NewService(ServiceOptions{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := service.FetchDetail(context.Background(), server.URL+"/ABF-055", "")
	if err != nil {
		t.Fatalf("fetch detail: %v", err)
	}
	if result.Metadata.GID != "12345" || result.FilmData.Title != "ABF-055 Sample" {
		t.Fatalf("unexpected detail result: %#v", result)
	}
}
