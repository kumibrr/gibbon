package ops

// Progress receives events while an operation runs. Implementations must be
// safe for concurrent use: operations call it from worker goroutines. A nil
// Progress is valid and means no reporting.
type Progress interface {
	// Plan reports how many repos the operation will touch, once known.
	Plan(total int)
	// Start is called immediately before work on repo begins.
	Start(repo string)
	// Warn reports an operation-level warning not tied to one Result.
	Warn(msg string)
	// Finish is called when work on r.Repo has ended, with the same Result
	// the operation returns for it.
	Finish(r Result)
}

// notify forwards to a Progress, ignoring a nil one.
type notify struct{ p Progress }

func (n notify) Plan(total int) {
	if n.p != nil {
		n.p.Plan(total)
	}
}
func (n notify) Start(repo string) {
	if n.p != nil {
		n.p.Start(repo)
	}
}
func (n notify) Warn(msg string) {
	if n.p != nil {
		n.p.Warn(msg)
	}
}
func (n notify) Finish(r Result) Result {
	if n.p != nil {
		n.p.Finish(r)
	}
	return r
}
