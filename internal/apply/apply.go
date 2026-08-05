// Package apply carries the theme into every corner of the desktop.
//
// Each target is an Applier: it says whether it is present on this machine
// (Detect) and carries the theme over (Apply). The two are separate because
// the TUI wants to show what WILL happen before anything does, and because
// an absent tool is a Skip, never a failure — this machine legitimately runs
// only one of the three compositors at a time.
package apply

import (
	"sync"

	"github.com/lvim-tech/themer/internal/config"
	"github.com/lvim-tech/themer/internal/theme"
)

type Status int

const (
	StatusPending Status = iota
	StatusRunning
	StatusOK
	StatusSkipped
	StatusFailed
)

type Applier interface {
	Name() string
	// Detect reports whether Apply would do anything here, with the reason
	// when it would not.
	Detect() (bool, string)
	// Apply carries the theme over and says what it did.
	Apply(t theme.Theme) (string, error)
}

// Result is one applier's outcome, streamed to the TUI as it lands.
type Result struct {
	Index  int
	Name   string
	Status Status
	Note   string
}

// All builds the applier list in application order.
//
// The order among the declared targets is THEIRS, by priority — see
// config.Target.Priority. It used to be decided here, and then, once the
// definitions moved into a directory, by filename, which put a compositor
// ahead of the state file: alphabetical order is arbitrary with respect to a
// decision, and a comment claiming otherwise was simply wrong for a while.
//
// The two remaining Go appliers go first because they are still Go. Neither
// repaints anything a failure afterwards would misrepresent — neovim writes
// two files and GTK's own steps stop at the first error — so nothing rests on
// where they sit. They lose the exemption the day they become definitions.
func All(cfg config.Config) []Applier {
	var appliers []Applier
	for _, t := range cfg.Targets {
		appliers = append(appliers, NewTarget(t))
	}
	return appliers
}

// Prefetcher is an applier that can say, before it runs, which URLs it will
// need. Run downloads them all first so the applying itself needs no network.
type Prefetcher interface {
	URLs(t theme.Theme) []string
}

// Run applies the theme through every applier, reporting each start and each
// outcome on the channel. It never stops early: one broken target must not
// leave the desktop half-switched any further than it already is.
//
// TWO PHASES, and the split is the point.
//
// Downloading is what a switch mostly is now — better than thirty files, each a
// round trip — and done one after another it took eight seconds of waiting for
// almost no work. So every URL is fetched first, CONCURRENTLY, into memory.
//
// Applying then runs SEQUENTIALLY, in the order the targets were declared,
// because that order carries two decisions worth keeping: the state file goes
// first, since everything downstream reads it, and the compositors go last,
// since they repaint the most visibly and a repaint before a failure would lie
// about how far the switch got. Running them concurrently threw both away.
//
// The split buys a third thing neither arrangement had: a download that fails
// is known BEFORE any file is touched, instead of halfway through with the
// earlier targets already applied.
func Run(appliers []Applier, t theme.Theme, results chan<- Result) {
	fetched := prefetch(appliers, t)

	for i, a := range appliers {
		if ta, ok := a.(*TargetApplier); ok {
			ta.fetched = fetched
		}
		ok, why := a.Detect()
		if !ok {
			results <- Result{Index: i, Name: a.Name(), Status: StatusSkipped, Note: why}
			continue
		}
		results <- Result{Index: i, Name: a.Name(), Status: StatusRunning}
		note, err := a.Apply(t)
		if err != nil {
			results <- Result{Index: i, Name: a.Name(), Status: StatusFailed, Note: err.Error()}
			continue
		}
		results <- Result{Index: i, Name: a.Name(), Status: StatusOK, Note: note}
	}
	close(results)
}

// prefetch downloads every URL the appliers will ask for, all at once.
//
// A URL that fails is simply absent from the result; the operation that wanted
// it then fetches it itself and reports the failure with its own target's name,
// which is what a reader needs to see. Downloading here is an optimisation, not
// a second code path.
func prefetch(appliers []Applier, t theme.Theme) map[string][]byte {
	seen := map[string]bool{}
	var urls []string
	for _, a := range appliers {
		p, ok := a.(Prefetcher)
		if !ok {
			continue
		}
		for _, u := range p.URLs(t) {
			if !seen[u] {
				seen[u] = true
				urls = append(urls, u)
			}
		}
	}

	var (
		mu  sync.Mutex
		out = make(map[string][]byte, len(urls))
		wg  sync.WaitGroup
	)
	for _, u := range urls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := download(u)
			if err != nil {
				return
			}
			mu.Lock()
			out[u] = b
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}
