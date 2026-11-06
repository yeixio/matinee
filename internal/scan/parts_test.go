package scan

import "testing"

func TestPart(t *testing.T) {
	cases := []struct {
		path string
		rest string
		part int
	}{
		{"/m/Heat (1995)/Heat (1995) CD1.avi", "Heat (1995)", 1},
		{"/m/Heat (1995)/Heat (1995) - cd2.avi", "Heat (1995)", 2},
		{"/m/Heat.1995.CD1.XviD-GRP.avi", "Heat.1995 XviD-GRP", 1},
		{"/m/Heat (1995)/Heat (1995) [Disc 2].mkv", "Heat (1995)", 2},
		{"/m/Heat (1995)/Heat (1995) DVD1.mkv", "Heat (1995)", 1},
		{"/m/Heat (1995)/heat-disk2.mkv", "heat", 2},
		{"/m/Heat.1995.Part1.avi", "Heat.1995", 1},
		{"/m/Heat (1995)/Heat pt2.avi", "Heat", 2},
		{"/m/Heat (1995)/Heat - Part 2.avi", "Heat", 2},
		// Parts of a title, not of a file.
		{"/m/Harry Potter and the Deathly Hallows Part 1 (2010)/Harry Potter and the Deathly Hallows Part 1 (2010).mkv", "Harry Potter and the Deathly Hallows Part 1 (2010)", 0},
		{"/m/Harry.Potter.and.the.Deathly.Hallows.Part.2.2011.1080p.mkv", "Harry.Potter.and.the.Deathly.Hallows.Part.2.2011.1080p", 0},
		{"/m/Dune Part Two (2024).mkv", "Dune Part Two (2024)", 0},
		{"/m/Kill Bill/Kill Bill Part 1.mkv", "Kill Bill Part 1", 0},
		{"/m/CD1.avi", "CD1", 0},
		{"/m/Heat (1995)/Heat (1995).mkv", "Heat (1995)", 0},
	}
	for _, c := range cases {
		rest, part := Part(c.path)
		if rest != c.rest || part != c.part {
			t.Errorf("Part(%q) = %q, %d; want %q, %d", c.path, rest, part, c.rest, c.part)
		}
	}
	if got := ParseMovie("/m/Heat CD1.avi"); got.Title != "Heat" {
		t.Errorf("ParseMovie drops the part: %+v", got)
	}
	a, _ := PartKey("/m/Heat.1995.CD1.XviD-GRP.avi")
	b, _ := PartKey("/m/Heat.1995.CD2.XviD-GRP.avi")
	if a == "" || a != b {
		t.Errorf("keys differ: %q %q", a, b)
	}
}
