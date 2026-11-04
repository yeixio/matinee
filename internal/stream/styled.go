package stream

import (
	"strconv"
	"strings"
)

// StyledFilter is the filter that draws an ASS/SSA track with libass, with
// the fonts attached to the file. libass reads the track from the start of
// the file, so the picture's clock is moved to the file's while it draws and
// back again after, as a transcode that starts partway in begins at zero.
// stream is the track's index among the file's subtitle streams, or -1 for a
// file that holds only the track.
func StyledFilter(path string, stream int, startSec float64) string {
	f := "subtitles=filename=" + filterValue(path)
	if stream >= 0 {
		f += ":si=" + strconv.Itoa(stream)
	}
	if startSec <= 0 {
		return f
	}
	at := strconv.FormatFloat(startSec, 'f', 3, 64)
	return "setpts=PTS+" + at + "/TB," + f + ",setpts=PTS-STARTPTS"
}

// filterValue quotes a value for a filter's options and then for the graph
// around it, so any path works: the option level ends at ':' and treats '\”
// and '\\' as special, and the graph level does the same with '[],;' too.
func filterValue(v string) string {
	opt := "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
	var b strings.Builder
	for _, r := range opt {
		if strings.ContainsRune(`\'[],;`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
