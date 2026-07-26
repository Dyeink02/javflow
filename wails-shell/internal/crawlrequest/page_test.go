package crawlrequest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBuildPageRequestHeadersPrefersManualCookie(t *testing.T) {
	headers := BuildPageRequestHeaders(BuildPageRequestHeadersOptions{
		RequestHeaders:      map[string]string{"Accept": "text/html"},
		ConfigCookie:        "foo=bar",
		CookieOverride:      "override=1",
		CloudflareCookies:   "cf=1",
		DefaultCookieHeader: DefaultCookieHeader,
	})

	if headers["Cookie"] != "foo=bar" {
		t.Fatalf("expected manual cookie, got %q", headers["Cookie"])
	}
	if headers["Accept"] != "text/html" {
		t.Fatalf("expected Accept header to remain, got %#v", headers)
	}
}

func TestPageResponseClassifiers(t *testing.T) {
	if IsUsablePageResponse(PageResponse{StatusCode: 200, Body: "age verification javbus"}) {
		t.Fatalf("age verification page should not be usable")
	}
	if IsUsablePageResponse(PageResponse{StatusCode: 503, Body: "Just a moment..."}) {
		t.Fatalf("cloudflare challenge page should not be usable")
	}
	if !IsUsablePageResponse(PageResponse{StatusCode: 200, Body: "<html>ok</html>"}) {
		t.Fatalf("normal html should be usable")
	}
}

func TestDriverVerificationQuizClassifier(t *testing.T) {
	body := `<form method="POST" action="driver-verify.php?referer=https://www.javbus.com/star/test">
<input type="radio" name="userAnswers[9]" value="A">
<button type="submit" name="submit" value="question">submit</button>
</form>`
	if !IsDriverVerificationQuizResponse(body) {
		t.Fatal("expected driver verification questionnaire to be detected")
	}
	if got := DescribePageFallbackReason(&PageResponse{StatusCode: 200, Body: body}); got != "常规请求命中问答验证页" {
		t.Fatalf("unexpected fallback reason: %q", got)
	}
	if IsDriverVerificationQuizResponse(`<div id="ageVerify"><input type="checkbox"></div>`) {
		t.Fatal("legacy age checkbox must not be classified as questionnaire")
	}
}

func TestVerificationFormValuesSelectsChallengeForm(t *testing.T) {
	body := `<form action="/search"><input name="q" value="ignored"></form>
<div id="ageVerify"><form method="post" action=""><input type="checkbox"><input type="submit" name="Submit" value="confirm"></form></div>`
	formURL, values, found, err := verificationFormValues("https://www.javbus.com/doc/driver-verify?referer=x", body)
	if err != nil || !found {
		t.Fatalf("expected legacy verification form, found=%v err=%v", found, err)
	}
	if formURL != "https://www.javbus.com/doc/driver-verify?referer=x" || values.Get("Submit") != "confirm" {
		t.Fatalf("unexpected legacy form extraction: url=%q values=%v", formURL, values)
	}

	body = `<form action="/search"><input name="q" value="ignored"></form>
<form method="POST" action="driver-verify.php?referer=https://www.javbus.com/star/vb3">
<input type="radio" name="userAnswers[1]" value="A"><input type="radio" name="userAnswers[1]" value="B">
<button type="submit" name="submit" value="question">send</button></form>`
	formURL, values, found, err = verificationFormValues("https://www.javbus.com/doc/driver-verify", body)
	if err != nil || !found || values.Get("userAnswers[1]") != "B" || values.Get("submit") != "question" {
		t.Fatalf("unexpected questionnaire extraction: url=%q values=%v found=%v err=%v", formURL, values, found, err)
	}
}

func TestGetPageCompletesTwoStepAgeVerification(t *testing.T) {
	sharedBrowserSessionCookies.Lock()
	sharedBrowserSessionCookies.header = ""
	sharedBrowserSessionCookies.Unlock()

	quiz := `<title>Age Verification JavBus</title><form method="POST" action="driver-verify.php?referer=/star/test">
<input type="radio" name="userAnswers[1]" value="A"><input type="radio" name="userAnswers[1]" value="B">
<input type="radio" name="userAnswers[4]" value="A"><input type="radio" name="userAnswers[4]" value="C">
<button type="submit" name="submit" value="question">send</button></form>`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/star/test":
			if cookie, err := request.Cookie("verified"); err == nil && cookie.Value == "1" {
				_, _ = writer.Write([]byte(`<a class="movie-box" href="/TEST-001">TEST-001</a>`))
				return
			}
			http.Redirect(writer, request, "/doc/driver-verify?referer=/star/test", http.StatusFound)
		case "/doc/driver-verify":
			if request.Method == http.MethodPost {
				http.SetCookie(writer, &http.Cookie{Name: "stage", Value: "quiz", Path: "/"})
				_, _ = writer.Write([]byte(quiz))
				return
			}
			if cookie, err := request.Cookie("stage"); err == nil && cookie.Value == "quiz" {
				_, _ = writer.Write([]byte(quiz))
				return
			}
			_, _ = writer.Write([]byte(`<title>Age Verification JavBus</title><form id="form1" method="post"><input type="submit" name="Submit" value="confirm"></form>`))
		case "/doc/driver-verify.php":
			_ = request.ParseForm()
			if request.Form.Get("userAnswers[1]") != "B" || request.Form.Get("userAnswers[4]") != "C" || request.Form.Get("submit") != "question" {
				http.Error(writer, "wrong answers", http.StatusBadRequest)
				return
			}
			http.SetCookie(writer, &http.Cookie{Name: "verified", Value: "1", Path: "/"})
			_, _ = writer.Write([]byte(quiz))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := NewClient(PageRequestOptions{Timeout: 5 * time.Second, RetryCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	response, err := client.GetPage(context.Background(), server.URL+"/star/test", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Body, "movie-box") {
		t.Fatalf("expected verified target page, got url=%q body=%q", response.URL, response.Body)
	}
}

func TestLiveJavBusAgeVerification(t *testing.T) {
	if os.Getenv("JAV_AUTO_LIVE_TEST") != "1" {
		t.Skip("set JAV_AUTO_LIVE_TEST=1 to run against JavBus")
	}
	sharedBrowserSessionCookies.Lock()
	sharedBrowserSessionCookies.header = ""
	sharedBrowserSessionCookies.Unlock()

	const workers = 5
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			client, err := NewClient(PageRequestOptions{
				Proxy:      os.Getenv("JAV_AUTO_LIVE_PROXY"),
				Timeout:    45 * time.Second,
				RetryCount: 1,
			})
			if err != nil {
				errors <- err
				return
			}
			defer client.Close()
			response, err := client.GetPage(context.Background(), "https://www.javbus.com/star/vb3", "")
			if err == nil && (IsAgeVerificationResponse(response.Body) || !strings.Contains(response.Body, "movie-box")) {
				err = fmt.Errorf("expected actress index page, url=%q status=%d", response.URL, response.StatusCode)
			}
			errors <- err
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestCookieValidation(t *testing.T) {
	headers := map[string]string{}
	if !SetCookieHeader(headers, "a=1; b=2") {
		t.Fatalf("expected valid cookie string")
	}
	if headers["Cookie"] != "a=1; b=2" {
		t.Fatalf("unexpected cookie header: %#v", headers)
	}
	if SetCookieHeader(headers, "bad-cookie") {
		t.Fatalf("expected invalid cookie string")
	}
}

func TestGetXMLHttpRequestWithRefererUsesDetailPageReferer(t *testing.T) {
	var receivedReferer string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedReferer = request.Header.Get("Referer")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	}))
	defer server.Close()

	client, err := NewClient(PageRequestOptions{})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	detailURL := server.URL + "/SNOS-183"
	_, err = client.GetXMLHttpRequestWithReferer(context.Background(), server.URL+"/ajax/uncledatoolsbyajax.php?gid=1", detailURL)
	if err != nil {
		t.Fatalf("GetXMLHttpRequestWithReferer: %v", err)
	}
	if receivedReferer != detailURL {
		t.Fatalf("expected referer %q, got %q", detailURL, receivedReferer)
	}
}
