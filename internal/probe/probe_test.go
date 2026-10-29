package probe

import "testing"

// An audiobook's chapters come back in order with their titles; chapters
// ffprobe can't place are left out.
func TestParseChapters(t *testing.T) {
	r, err := parse([]byte(`{
		"format": {"format_name": "mov,mp4,m4a", "duration": "120.5", "tags": {"title": "Book"}},
		"streams": [{"codec_type": "audio", "codec_name": "aac"}],
		"chapters": [
			{"start_time": "0.000000", "end_time": "60.250000", "tags": {"title": "Opening Credits"}},
			{"start_time": "60.250000", "end_time": "120.500000", "tags": {"TITLE": "Chapter 1"}},
			{"start_time": "bad", "end_time": "130"}
		]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Chapters) != 2 || r.Chapters[0] != (Chapter{"Opening Credits", 0, 60250}) || r.Chapters[1] != (Chapter{"Chapter 1", 60250, 120500}) {
		t.Fatalf("chapters: %+v", r.Chapters)
	}
}
