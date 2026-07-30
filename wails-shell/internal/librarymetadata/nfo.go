// Ownership summary:
//   This file writes Emby/Jellyfin-compatible NFO XML from normalized movie metadata.
//
// File map for maintainers:
//   1) NFOData, NFOActor, and NFOPoster types.
//   2) NFO generation from MovieInfo.
//   3) XML encoding and pretty-print helpers.
//
package librarymetadata

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// NFOData is the XML payload used by Emby/Jellyfin for a movie item.
type NFOData struct {
	XMLName       xml.Name   `xml:"movie"`
	Title         string     `xml:"title"`
	OriginalTitle string     `xml:"originaltitle"`
	SortTitle     string     `xml:"sorttitle"`
	Plot          string     `xml:"plot"`
	Outline       string     `xml:"outline"`
	Year          int        `xml:"year,omitempty"`
	Premiered     string     `xml:"premiered,omitempty"`
	ReleaseDate   string     `xml:"releasedate,omitempty"`
	Runtime       int        `xml:"runtime,omitempty"`
	MPAA          string     `xml:"mpaa"`
	Genres        []string   `xml:"genre"`
	Studio        string     `xml:"studio,omitempty"`
	Director      string     `xml:"director,omitempty"`
	Actors        []NFOActor `xml:"actor"`
	Thumb         []NFOThumb `xml:"thumb"`
	Fanart        *Fanart    `xml:"fanart,omitempty"`
}

type NFOActor struct {
	Name  string `xml:"name"`
	Type  string `xml:"type,omitempty"`
	Role  string `xml:"role,omitempty"`
	Thumb string `xml:"thumb,omitempty"`
}

type NFOThumb struct {
	Aspect string `xml:"aspect,attr,omitempty"`
	Value  string `xml:",chardata"`
}

type Fanart struct {
	Thumbs []NFOThumb `xml:"thumb"`
}

// BuildNFO renders a Kodi/Emby/Jellyfin movie NFO from scraped metadata.
func BuildNFO(info *MovieInfo) ([]byte, error) {
	if info == nil {
		return nil, fmt.Errorf("movie info is nil")
	}

	actorImageMap := make(map[string]string, len(info.ActorImages))
	for _, ai := range info.ActorImages {
		if ai.Name != "" && ai.URL != "" {
			actorImageMap[ai.Name] = ai.URL
		}
	}

	actors := make([]NFOActor, 0, len(info.Actors))
	for _, name := range info.Actors {
		if name = strings.TrimSpace(name); name != "" {
			actors = append(actors, NFOActor{
				Name:  name,
				Type:  "Actor",
				Thumb: actorImageMap[name],
			})
		}
	}

	genres := make([]string, 0, len(info.Genres))
	for _, g := range info.Genres {
		if tag := strings.TrimSpace(g); tag != "" {
			genres = append(genres, tag)
		}
	}

	thumbs := make([]NFOThumb, 0, 2)
	if info.CoverURL != "" {
		thumbs = append(thumbs, NFOThumb{Aspect: "poster", Value: info.CoverURL})
	}
	if info.ThumbURL != "" {
		thumbs = append(thumbs, NFOThumb{Aspect: "thumb", Value: info.ThumbURL})
	}

	var fanart *Fanart
	if info.BackdropURL != "" {
		fanart = &Fanart{
			Thumbs: []NFOThumb{{Value: info.BackdropURL}},
		}
	}

	title := strings.TrimSpace(info.Title)
	number := strings.TrimSpace(info.Number)
	if number != "" && !strings.HasPrefix(strings.ToUpper(title), strings.ToUpper(number)+" ") && !strings.EqualFold(title, number) {
		title = number + " " + title
	}

	data := NFOData{
		Title:         title,
		OriginalTitle: title,
		SortTitle:     info.Number,
		Plot:          info.Plot,
		Outline:       info.Outline,
		Year:          info.Year,
		Premiered:     info.ReleaseDate,
		ReleaseDate:   info.ReleaseDate,
		Runtime:       info.Runtime,
		MPAA:          "XXX",
		Genres:        genres,
		Studio:        info.Studio,
		Director:      info.Director,
		Actors:        actors,
		Thumb:         thumbs,
		Fanart:        fanart,
	}

	output, err := xml.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	header := []byte(xml.Header)
	return append(header, output...), nil
}
