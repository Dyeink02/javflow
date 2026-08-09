package actresslookup

import (
	"strings"
	"testing"

	"javflow/internal/contracts/subscriptiontarget"
)

func TestParseStarPageIncludesAvatarAndWorks(t *testing.T) {
	htmlText := `<html><head><title>测试女优 - 女优 - 影片</title></head><body>
<div class="avatar-box"><img src="/pics/actress/test_a.jpg" title="测试女优"></div>
<div>已有磁力 2 部，可切换至全部影片 3 部</div>
<a class="movie-box" href="/ABC-001"><img src="/pics/thumb/a.jpg" title="作品一"><div class="photo-info"><date>ABC-001</date> / <date>2026-07-01</date></div></a>
</body></html>`
	page, err := parseStarPage(htmlText, "https://www.javbus.com/star/test")
	if err != nil {
		t.Fatal(err)
	}
	if page.AvatarURL != "https://www.javbus.com/pics/actress/test_a.jpg" {
		t.Fatalf("unexpected avatar: %s", page.AvatarURL)
	}
	if len(page.Works) != 1 || page.Works[0].Code != "ABC-001" || page.Works[0].ReleaseDate != "2026-07-01" {
		t.Fatalf("unexpected works: %+v", page.Works)
	}
	if page.Works[0].URL != "https://www.javbus.com/ABC-001" {
		t.Fatalf("unexpected work URL: %s", page.Works[0].URL)
	}
}

func TestParseMinnanoProfileUsesOnlyRealFields(t *testing.T) {
	htmlText := `<html><head><title>测试女优（テスト / Test Actor）AV女優プロフィール - みんなのAV.com</title>
<script type="application/ld+json">{"@type":"Person","name":"测试女优","alternateName":"テスト","additionalName":"Test Actor","image":"/p_actress_125_125/001/1.jpg","birthDate":"2000-02-03","birthPlace":{"name":"東京都"},"affiliation":{"name":"Test Production"},"gender":"Female"}</script></head><body>
<div class="act-profile"><table><tr><td><span>サイズ</span><p>T170 / B101(Jカップ) / W59 / H91 / S</p></td></tr><tr><td><span>公式サイト</span><p><a href="https://example.com">https://example.com</a></p></td></tr></table></div>
<section class="act-video-list"><a href="/av123456.html"><img data-src="/p_package/2607/123456.jpg" alt="真实作品"><h4 class="video-title">真实作品</h4><p>2026/07/04</p></a></section>
</body></html>`
	profile, err := parseMinnanoProfile(htmlText, "https://www.minnano-av.com/actress1.html", "测试女优")
	if err != nil {
		t.Fatal(err)
	}
	if profile.AvatarURL != "https://www.minnano-av.com/p_actress_125_125/001/1.jpg" {
		t.Fatalf("unexpected avatar: %s", profile.AvatarURL)
	}
	if profile.Fields["生日"] != "2000-02-03" || profile.Fields["性别"] != "Female" {
		t.Fatalf("unexpected structured fields: %+v", profile.Fields)
	}
	if !strings.Contains(profile.Fields["身材"], "T170") || len(profile.Works) != 1 {
		t.Fatalf("unexpected profile body/works: %+v %+v", profile.Fields, profile.Works)
	}
	if profile.Works[0].Code != "AV123456" || profile.Works[0].ReleaseDate != "2026-07-04" {
		t.Fatalf("unexpected work: %+v", profile.Works[0])
	}
}

func TestParseMinnanoProfileRejectsNameMismatch(t *testing.T) {
	htmlText := `<html><head><title>其他演员（別名）AV女優プロフィール</title></head><body><div class="act-profile"><h2>其他演员（別名）</h2></div></body></html>`
	if _, err := parseMinnanoProfile(htmlText, "https://www.minnano-av.com/actress2.html", "目标演员"); err == nil {
		t.Fatal("expected strict profile-name mismatch")
	}
}

func TestParseMinnanoProfileExcludesRecommendedWorks(t *testing.T) {
	htmlText := `<html><head><title>瀬戸環奈（せとかんな）AV女優プロフィール</title></head><body>
<div class="act-profile"><h2>瀬戸環奈（せとかんな）</h2></div>
<div class="recommended-videos"><div class="video-item"><a href="av999999.html"><img src="p_package/9999/999999.jpg" alt="推荐作品"></a></div></div>
<div class="act-video-list"><a href="av123456.html"><img data-src="p_package/2607/123456.jpg" alt="瀬戸環奈作品"><h3 class="ttl">瀬戸環奈作品</h3><span>2026/07/01</span></a></div>
</body></html>`
	profile, err := parseMinnanoProfile(htmlText, "https://www.minnano-av.com/actress125605.html", "瀬戸環奈")
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Works) != 1 || profile.Works[0].Code != "AV123456" {
		t.Fatalf("recommended work leaked into actor list: %+v", profile.Works)
	}
}

func TestSelectCanonicalWorksPrefersJAVBusNumbers(t *testing.T) {
	primary := []subscriptiontarget.ActressWork{{Code: "ABCD-123", URL: "https://www.javbus.com/ABCD-123"}}
	secondary := []subscriptiontarget.ActressWork{{Code: "AV720737", URL: "https://www.minnano-av.com/av720737.html"}}
	works := selectCanonicalWorks(primary, secondary)
	if len(works) != 1 || works[0].Code != "ABCD-123" {
		t.Fatalf("expected JAVBus work to remain canonical: %+v", works)
	}
}

func TestDistinctPromotionImagesExcludesAvatarAndCanonicalDuplicates(t *testing.T) {
	avatar := "https://images.example.test/actor.jpg?size=small"
	photos := distinctPromotionImages(avatar,
		[]string{
			"https://images.example.test/actor.jpg?size=large",
			"https://images.example.test/promo-a.jpg?width=400",
			"https://images.example.test/promo-a.jpg?width=1200",
			"https://images.example.test/promo-b.jpg#gallery",
		},
	)
	if len(photos) != 2 {
		t.Fatalf("expected two distinct non-avatar publicity images, got %#v", photos)
	}
	if photos[0] != "https://images.example.test/promo-a.jpg?width=400" || photos[1] != "https://images.example.test/promo-b.jpg#gallery" {
		t.Fatalf("unexpected public image order: %#v", photos)
	}
}
