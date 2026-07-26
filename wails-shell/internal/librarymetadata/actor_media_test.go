package librarymetadata

import (
	"testing"

	"github.com/lib/pq"
	metatubemodel "github.com/metatube-community/metatube-sdk-go/model"
)

func TestSelectActorMediaResultPrefersExactAlias(t *testing.T) {
	wrong := &metatubemodel.ActorSearchResult{
		ID: "1", Name: "相似名称", Provider: "test", Homepage: "https://example.test/1",
	}
	exactAlias := &metatubemodel.ActorSearchResult{
		ID: "2", Name: "English Name", Provider: "test", Homepage: "https://example.test/2",
		Aliases: pq.StringArray{"小島みなみ"}, Images: pq.StringArray{"https://example.test/avatar.jpg"},
	}
	selected := selectActorMediaResult([]*metatubemodel.ActorSearchResult{wrong, exactAlias}, "小島 みなみ")
	if selected != exactAlias {
		t.Fatalf("expected exact alias match, got %+v", selected)
	}
}
