// Package apply carries the theme into every corner of the desktop.
//
// Each target is an Applier: it says whether it is present on this machine
// (Detect) and carries the theme over (Apply). The two are separate because
// the TUI wants to show what WILL happen before anything does, and because
// an absent tool is a Skip, never a failure — this machine legitimately runs
// only one of the three compositors at a time.
package apply

import (
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
	Apply(t theme.Theme, p theme.Palette) (string, error)
}

// Result is one applier's outcome, streamed to the TUI as it lands.
type Result struct {
	Index  int
	Name   string
	Status Status
	Note   string
}

// All builds the applier list in application order. The state file goes
// first because everything downstream — new shells above all — reads it;
// the declarative targets (the compositors among them) go last because they
// repaint the most visibly, and a repaint before a failure would lie about
// how far the switch got.
func All(cfg config.Config) []Applier {
	appliers := []Applier{
		NewStateFile(cfg.StateFile),
		NewClipack(cfg.ClipackBase),
		NewKitty(),
		NewTmux(cfg.ClipackBase),
		NewWezterm(),
		NewGTK(),
	}
	for _, t := range cfg.Targets {
		appliers = append(appliers, NewTarget(t, cfg.Roles))
	}
	return appliers
}

// Run applies the theme through every applier, reporting each start and each
// outcome on the channel. It never stops early: one broken target must not
// leave the desktop half-switched any further than it already is.
func Run(appliers []Applier, t theme.Theme, p theme.Palette, results chan<- Result) {
	for i, a := range appliers {
		ok, why := a.Detect()
		if !ok {
			results <- Result{Index: i, Name: a.Name(), Status: StatusSkipped, Note: why}
			continue
		}
		results <- Result{Index: i, Name: a.Name(), Status: StatusRunning}
		note, err := a.Apply(t, p)
		if err != nil {
			results <- Result{Index: i, Name: a.Name(), Status: StatusFailed, Note: err.Error()}
			continue
		}
		results <- Result{Index: i, Name: a.Name(), Status: StatusOK, Note: note}
	}
	close(results)
}
