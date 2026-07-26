package crawlrequest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const samplePageHTML = `
<html>
  <head>
    <meta property="og:title" content="ABF-055 Sample" />
    <meta property="og:image" content="https://img.example.com/abf055.jpg" />
  </head>
  <body>
    <script>
      var gid = 12345;
      var uc = 9;
      var img = 'https:\/\/img.example.com\/abf055.jpg';
    </script>
    <a class="movie-box" href="/ABF-055"></a>
    <a class="movie-box" href="/ABW-006"></a>
    <span class="genre"><label><a>剧情</a></label></span>
    <div class="star-name"><a>三上悠亜</a></div>
  </body>
</html>
`

func TestClientFetchIndexPageLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write([]byte(samplePageHTML))
	}))
	defer server.Close()

	client, err := NewClient(PageRequestOptions{})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	links, response, err := client.FetchIndexPageLinks(context.Background(), server.URL, "")
	if err != nil {
		t.Fatalf("fetch links: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: %#v", response)
	}
	if len(links) != 2 {
		t.Fatalf("unexpected links: %#v", links)
	}
}

func TestClientFetchFilmData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write([]byte(samplePageHTML))
	}))
	defer server.Close()

	client, err := NewClient(PageRequestOptions{})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	filmData, _, err := client.FetchFilmData(context.Background(), server.URL+"/ABF-055", "")
	if err != nil {
		t.Fatalf("fetch film data: %v", err)
	}
	if filmData.Title != "ABF-055 Sample" {
		t.Fatalf("unexpected film data: %#v", filmData)
	}
	if filmData.SourceLink != server.URL+"/ABF-055" {
		t.Fatalf("unexpected source link: %#v", filmData)
	}
	if filmData.CoverImage == "" || len(filmData.Actress) != 1 {
		t.Fatalf("unexpected film data detail: %#v", filmData)
	}
}
