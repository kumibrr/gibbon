package discover

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// Resolve maps user arguments onto repos. Each argument may be a full id, a
// bare directory name unique across repos, a glob against ids, or "group/"
// meaning every repo under that group. Results are deduplicated and sorted.
func Resolve(repos []Repo, args []string) ([]Repo, error) {
	byID := map[string]Repo{}
	byName := map[string][]Repo{}
	for _, r := range repos {
		byID[r.ID] = r
		byName[path.Base(r.ID)] = append(byName[path.Base(r.ID)], r)
	}
	seen := map[string]bool{}
	var out []Repo
	add := func(r Repo) {
		if !seen[r.ID] {
			seen[r.ID] = true
			out = append(out, r)
		}
	}
	for _, arg := range args {
		arg = strings.TrimPrefix(strings.TrimPrefix(arg, "./"), "base/")
		switch {
		case strings.HasSuffix(arg, "/"):
			prefix := arg
			n := 0
			for _, r := range repos {
				if strings.HasPrefix(r.ID, prefix) {
					add(r)
					n++
				}
			}
			if n == 0 {
				return nil, fmt.Errorf("no repos under group %q", arg)
			}
		case strings.ContainsAny(arg, "*?["):
			n := 0
			for _, r := range repos {
				if ok, _ := path.Match(arg, r.ID); ok {
					add(r)
					n++
				}
			}
			if n == 0 {
				return nil, fmt.Errorf("no repos match %q", arg)
			}
		default:
			if r, ok := byID[arg]; ok {
				add(r)
				continue
			}
			cands := byName[arg]
			switch len(cands) {
			case 1:
				add(cands[0])
			case 0:
				return nil, fmt.Errorf("no repo named %q", arg)
			default:
				ids := make([]string, len(cands))
				for i, c := range cands {
					ids[i] = c.ID
				}
				sort.Strings(ids)
				return nil, fmt.Errorf("%q is ambiguous; use one of: %s", arg, strings.Join(ids, ", "))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
