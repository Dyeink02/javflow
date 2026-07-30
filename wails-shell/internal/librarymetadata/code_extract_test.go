package librarymetadata

import "testing"

func TestExtractCodeFromFilename_BBAN452(t *testing.T) {
	cases := []struct {
		filename string
		wantCode string
		wantTags string
		wantPart string
	}{
		{"BBAN-452.strm", "BBAN-452", "", ""},
		{"BBAN-452-C.strm", "BBAN-452", "中文字幕", ""},
		{"BBAN-452-cd1.mp4", "BBAN-452", "", "cd1"},
		{"DASS-287-A.mp4.strm", "DASS-287", "", "A"},
		{"DASS-287-B.mp4.strm", "DASS-287", "", "B"},
		{"MIDD-820-1.mp4.strm", "MIDD-820", "", "1"},
		{"MIDD-820-2.mp4.strm", "MIDD-820", "", "2"},
		{"MIDD-820_DUP1.mp4.strm", "MIDD-820", "", "_DUP1"},
		{"MIDD-820_DUP2.mp4.strm", "MIDD-820", "", "_DUP2"},
		{"IPZZ-123.strm", "IPZZ-123", "", ""},
		{"FC2PPV-1234567.mp4", "FC2-PPV-1234567", "", ""},
		{"SSIS-999-C.strm", "SSIS-999", "中文字幕", ""},
	}

	for _, c := range cases {
		got := ExtractCodeFromFilename(c.filename)
		if got.Code != c.wantCode || got.Tags != c.wantTags || got.Part != c.wantPart {
			t.Errorf("ExtractCodeFromFilename(%q) = %+v; want code=%q tags=%q part=%q", c.filename, got, c.wantCode, c.wantTags, c.wantPart)
		}
	}
}
