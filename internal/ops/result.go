// Package ops implements gibbon's operations as pure functions returning
// per-repo results. Nothing in this package prints.
package ops

// Result is the outcome of one operation on one repo.
type Result struct {
	Repo     string   `json:"repo"`
	Action   string   `json:"action"`
	Err      error    `json:"-"`
	Error    string   `json:"error,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

func result(repo, action string, err error, warnings ...string) Result {
	r := Result{Repo: repo, Action: action, Err: err, Warnings: warnings}
	if err != nil {
		r.Error = err.Error()
		if r.Action == "" {
			r.Action = "failed"
		}
	}
	return r
}

// AnyFailed reports whether any result carries an error.
func AnyFailed(rs []Result) bool {
	for _, r := range rs {
		if r.Err != nil {
			return true
		}
	}
	return false
}
