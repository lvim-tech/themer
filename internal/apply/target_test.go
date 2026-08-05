package apply

import (
	"github.com/lvim-tech/themer/internal/theme"
)

// The theme every applier test switches to. There is no palette beside it any
// more: themer composes no colour, so a theme is its name and the two parts
// GTKName and NvimName are derived from.
var testTheme = theme.Theme{Name: "LvimEverforest_soft", Family: "everforest", Variant: "soft"}
