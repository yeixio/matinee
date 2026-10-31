package importer

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/yeixio/matinee/internal/store"
)

// maxPosterBytes is the largest poster brought over, as for covers an
// admin uploads.
const maxPosterBytes = 20 << 20

// Plan is a snapshot matched to Matinee's files with a set of path changes.
type Plan struct {
	snap    *Snapshot
	Changes []PathChange
	// leaves maps the other server's item to Matinee's files for it.
	leaves map[int64][]store.FileRef
	// containers maps its shows, seasons, artists and albums to Matinee's.
	containers map[int64]int64
	byGUID     map[string][]int64
	// Files counts the other server's files, and Matched the ones Matinee has.
	Files, Matched int
	// Unmatched lists a few files Matinee doesn't have, as examples.
	Unmatched []string
	// Suggested are path changes that would match more files.
	Suggested []PathChange
}

// NewPlan matches a snapshot's files to Matinee's.
func NewPlan(snap *Snapshot, files []store.FileRef, changes []PathChange) *Plan {
	known := make(map[string]store.FileRef, len(files))
	byName := map[string][]string{}
	for _, f := range files {
		known[f.Path] = f
		n := base(f.Path)
		if len(byName[n]) < 8 {
			byName[n] = append(byName[n], f.Path)
		}
	}
	p := &Plan{snap: snap, Changes: changes, leaves: map[int64][]store.FileRef{}, containers: map[int64]int64{}, byGUID: map[string][]int64{}}
	for id, it := range snap.Items {
		if it.GUID != "" {
			p.byGUID[it.GUID] = append(p.byGUID[it.GUID], id)
		}
	}
	var all []string
	for id, paths := range snap.Files {
		for _, path := range paths {
			p.Files++
			all = append(all, path)
			ref, ok := known[MapPath(path, changes)]
			if !ok {
				if len(p.Unmatched) < 5 {
					p.Unmatched = append(p.Unmatched, path)
				}
				continue
			}
			p.Matched++
			p.leaves[id] = append(p.leaves[id], ref)
			// The file's season and show (or album and artist) are Matinee's
			// season and show for the other server's.
			if parent := snap.Items[snap.Items[id].ParentID]; parent != nil && ref.ParentID != 0 {
				if _, ok := p.containers[parent.ID]; !ok {
					p.containers[parent.ID] = ref.ParentID
				}
				if gp := snap.Items[parent.ParentID]; gp != nil && ref.GrandparentID != 0 {
					if _, ok := p.containers[gp.ID]; !ok {
						p.containers[gp.ID] = ref.GrandparentID
					}
				}
			}
		}
	}
	if p.Matched < p.Files {
		p.Suggested = SuggestChanges(all, func(s string) bool { _, ok := known[s]; return ok }, byName, changes)
	}
	return p
}

// targets lists Matinee's items for one of the other server's.
func (p *Plan) targets(id int64) []int64 {
	if refs, ok := p.leaves[id]; ok {
		out := make([]int64, len(refs))
		for i, r := range refs {
			out[i] = r.ID
		}
		return out
	}
	if c, ok := p.containers[id]; ok {
		return []int64{c}
	}
	return nil
}

// AccountSummary is how much one account watched there, and how much of it
// Matinee has.
type AccountSummary struct {
	Account
	Watched    int `json:"watched"`
	InProgress int `json:"inProgress"`
	Found      int `json:"found"` // of those, on Matinee
}

// Accounts sums up each account's history.
func (p *Plan) Accounts() []AccountSummary {
	byID := map[int64]*AccountSummary{}
	out := make([]AccountSummary, len(p.snap.Accounts))
	for i, a := range p.snap.Accounts {
		out[i] = AccountSummary{Account: a}
		byID[a.ID] = &out[i]
	}
	for _, v := range p.snap.Views {
		a := byID[v.AccountID]
		if a == nil {
			continue
		}
		if v.OffsetMs > 0 {
			a.InProgress++
		} else {
			a.Watched++
		}
		for _, id := range p.byGUID[v.GUID] {
			if len(p.leaves[id]) > 0 {
				a.Found++
				break
			}
		}
	}
	return out
}

// Collections counts the collections with something Matinee has.
func (p *Plan) Collections() int {
	n := 0
	for _, c := range p.snap.Collections {
		for _, id := range c.Items {
			if len(p.targets(id)) > 0 {
				n++
				break
			}
		}
	}
	return n
}

// posterTypes are the items whose own poster is brought over.
var posterTypes = map[int]bool{typeMovie: true, typeShow: true, typeSeason: true, typeArtist: true, typeAlbum: true}

type poster struct {
	file   string
	target int64
}

// posters finds the posters someone uploaded there, for items Matinee has.
func (p *Plan) posters() []poster {
	if p.snap.Location.DataDir == "" {
		return nil
	}
	var out []poster
	for id, it := range p.snap.Items {
		name, ok := strings.CutPrefix(it.Poster, "upload://")
		if !ok || !posterTypes[it.Type] || len(it.Hash) < 2 {
			continue
		}
		targets := p.targets(id)
		if len(targets) == 0 {
			continue
		}
		if f := p.posterFile(it.Hash, name); f != "" {
			out = append(out, poster{file: f, target: targets[0]})
		}
	}
	return out
}

// posterFile finds an uploaded poster in the item's metadata bundle:
// Metadata/<type>/<first character of hash>/<rest of hash>.bundle/Uploads/<name>.
func (p *Plan) posterFile(hash, name string) string {
	bundles, _ := filepath.Glob(filepath.Join(p.snap.Location.DataDir, "Metadata", "*", hash[:1], hash[1:]+".bundle"))
	want := filepath.FromSlash(name)
	for _, b := range bundles {
		f := filepath.Join(b, "Uploads", want)
		if info, err := os.Stat(f); err == nil && info.Mode().IsRegular() {
			return f
		}
		// Anywhere else in the bundle, by its file name.
		var found string
		filepath.WalkDir(b, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() == base(name) {
				found = path
				return filepath.SkipAll
			}
			return nil
		})
		if found != "" {
			return found
		}
	}
	return ""
}

// Posters counts the uploaded posters there are to bring over.
func (p *Plan) Posters() int { return len(p.posters()) }

// Options say what an import brings over, and for whom.
type Options struct {
	// Accounts maps the other server's accounts to Matinee's accounts or
	// profiles; accounts left out are skipped.
	Accounts    map[int64]int64
	Watched     bool
	Collections bool
	Posters     bool
	// Step is told what's done so far, out of the total, as the import goes.
	Step func(done, total int)
}

// Result says what an import did.
type Result struct {
	Watched         int `json:"watched"`         // marked watched
	InProgress      int `json:"inProgress"`      // given a place to resume from
	Kept            int `json:"kept"`            // left alone: watched more recently on Matinee
	Collections     int `json:"collections"`     // made or added to
	CollectionItems int `json:"collectionItems"` // titles put in them
	Posters         int `json:"posters"`
}

type bookView struct {
	ref  store.FileRef
	view View
}

// Apply brings the history, collections and posters over. Places and
// watched marks only replace Matinee's own when the other server's are
// newer, so running an import again, or after watching here, loses nothing.
func (p *Plan) Apply(ctx context.Context, s *store.Store, o Options) (Result, error) {
	var res Result
	var views []View
	if o.Watched {
		for _, v := range p.snap.Views {
			if _, ok := o.Accounts[v.AccountID]; ok {
				views = append(views, v)
			}
		}
	}
	var collections []Collection
	if o.Collections {
		collections = p.snap.Collections
	}
	var posters []poster
	if o.Posters {
		posters = p.posters()
	}
	total, done := len(views)+len(collections)+len(posters), 0
	step := func() {
		done++
		if o.Step != nil && (done%200 == 0 || done == total) {
			o.Step(done, total)
		}
	}

	books := map[int64]map[int64][]bookView{} // user → book → parts' views
	for _, v := range views {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		user := o.Accounts[v.AccountID]
		at := v.LastViewed
		if at <= 0 {
			at = 1 // unknown: older than anything on Matinee
		}
		for _, id := range p.byGUID[v.GUID] {
			for _, ref := range p.leaves[id] {
				if ref.Kind == "part" && ref.ParentKind == "book" {
					if books[user] == nil {
						books[user] = map[int64][]bookView{}
					}
					books[user][ref.ParentID] = append(books[user][ref.ParentID], bookView{ref, v})
					continue
				}
				e := store.ProgressEvent{ItemID: ref.ID, At: at}
				switch {
				case ref.Kind == "track":
					if v.Count == 0 {
						continue // a song's place isn't kept
					}
					e.Played = true
				case v.OffsetMs > 0:
					e.PositionMs = v.OffsetMs
				default:
					e.Played = true
				}
				ok, err := s.MergeProgress(ctx, user, e)
				if err != nil {
					return res, err
				}
				count(&res, ok, e.Played)
			}
		}
		step()
	}
	for user, byBook := range books {
		for bookID, parts := range byBook {
			e, ok := bookPlace(ctx, s, bookID, parts)
			if !ok {
				continue
			}
			applied, err := s.MergeBookProgress(ctx, user, e)
			if err != nil {
				return res, err
			}
			count(&res, applied, e.Played)
		}
	}

	if len(collections) > 0 {
		existing, err := s.Collections(ctx, 0)
		if err != nil {
			return res, err
		}
		for _, c := range collections {
			var ids []int64
			seen := map[int64]bool{}
			for _, id := range c.Items {
				for _, t := range p.targets(id) {
					if !seen[t] {
						seen[t] = true
						ids = append(ids, t)
					}
				}
			}
			if len(ids) > 0 {
				id := ""
				for _, e := range existing {
					if e.Shared && e.Kind == store.CollectionManual && strings.EqualFold(e.Name, c.Name) {
						id = e.ID
						break
					}
				}
				if id == "" {
					made, err := s.CreateCollection(ctx, store.SavedCollection{Name: c.Name, Kind: store.CollectionManual})
					if err != nil {
						return res, err
					}
					existing = append(existing, made)
					id = made.ID
				}
				for _, t := range ids {
					if err := s.AddToCollection(ctx, id, t); err != nil {
						return res, err
					}
				}
				res.Collections++
				res.CollectionItems += len(ids)
			}
			step()
		}
	}

	for _, ps := range posters {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if img, ok := readPoster(ps.file); ok {
			if err := s.SetChosenImage(ctx, ps.target, img); err != nil {
				return res, err
			}
			res.Posters++
		}
		step()
	}
	return res, nil
}

func count(res *Result, applied, played bool) {
	switch {
	case !applied:
		res.Kept++
	case played:
		res.Watched++
	default:
		res.InProgress++
	}
}

// bookPlace works out where someone is in a book from where they were in
// its parts: finished when every part is, otherwise in the part they
// played last.
func bookPlace(ctx context.Context, s *store.Store, bookID int64, views []bookView) (store.ProgressEvent, bool) {
	b, err := s.Book(ctx, 0, bookID)
	if err != nil || len(b.Parts) == 0 {
		return store.ProgressEvent{}, false
	}
	played := map[int64]bool{}
	var last bookView
	for _, v := range views {
		if v.view.Count > 0 && v.view.OffsetMs == 0 {
			played[v.ref.ID] = true
		}
		if v.view.LastViewed >= last.view.LastViewed {
			last = v
		}
	}
	at := max(last.view.LastViewed, 1)
	all := true
	for _, part := range b.Parts {
		if !played[part.ID] {
			all = false
		}
	}
	if all {
		return store.ProgressEvent{ItemID: bookID, Played: true, At: at}, true
	}
	for _, part := range b.Parts {
		if part.ID != last.ref.ID {
			continue
		}
		pos := part.StartMs + last.view.OffsetMs
		if last.view.OffsetMs == 0 && part.DurationMs != nil {
			pos = part.StartMs + *part.DurationMs // finished this part
		}
		return store.ProgressEvent{ItemID: bookID, PositionMs: pos, At: at}, pos > 0
	}
	return store.ProgressEvent{}, false
}

func readPoster(path string) (store.ChosenImage, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxPosterBytes || info.Size() == 0 {
		return store.ChosenImage{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return store.ChosenImage{}, false
	}
	ct := http.DetectContentType(data)
	switch ct {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return store.ChosenImage{Data: data, ContentType: ct, Source: "import"}, true
	}
	return store.ChosenImage{}, false
}

// Location is where the plan's snapshot was read from.
func (p *Plan) Location() Location { return p.snap.Location }
