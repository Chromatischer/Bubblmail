package ui

import (
	"fmt"
	"github.com/bubblmail/bubblmail/config"

	"github.com/bubblmail/bubblmail/ui/components"
	"github.com/bubblmail/bubblmail/ui/icons"
	"github.com/bubblmail/bubblmail/util"
	"github.com/charmbracelet/lipgloss"
)

// headerHeightRows is the fixed number of terminal rows the header occupies:
// one chrome row plus the rule that separates it from the content area.
// The header is one row. It used to be two: a row of content and a ─ rule under
// it. Nothing separates it from the list now except the weight of its own type,
// which is enough, and the row that bought went to the mail list.
const headerHeightRows = 1

// Header renders the top chrome: brand, account › folder breadcrumb, and the
// live counts and sync state on the right.
//
// It is deliberately two rows rather than three. The old third row carried
// nothing the breadcrumb row could not, and a row of vertical space is worth
// more to a mail list than a second heading.
type Header struct {
	theme         *config.Theme
	width         int
	activeAccount string
	activeFolder  string
	syncState     string // "", "syncing", "synced", "error"
	unread        int
	total         int
	filterLabel   string // non-empty when the list is showing a filtered subset
}

// NewHeader creates a new header component.
func NewHeader(theme *config.Theme) *Header {
	return &Header{theme: theme}
}

// SetWidth sets the header width.
func (h *Header) SetWidth(w int) { h.width = w }

// SetAccount updates the active account name.
func (h *Header) SetAccount(account string) { h.activeAccount = account }

// SetFolder updates the active folder name.
func (h *Header) SetFolder(folder string) { h.activeFolder = folder }

// SetSyncState sets the sync indicator state.
func (h *Header) SetSyncState(state string) { h.syncState = state }

// SetCounts updates the unread and total message counts shown on the right.
func (h *Header) SetCounts(unread, total int) {
	h.unread = unread
	h.total = total
}

// SetFilterLabel sets a short label describing an active filter (a search
// query, a smart folder). Pass "" when the list is unfiltered.
func (h *Header) SetFilterLabel(label string) { h.filterLabel = label }

// Height returns the rows the header occupies.
func (h *Header) Height() int { return headerHeightRows }

// View renders the header.
func (h *Header) View() string {
	theme := h.theme
	// No fill: the header is type on the terminal, like everything else.
	const bg = lipgloss.Color("")

	on := func(st lipgloss.Style) lipgloss.Style {
		if bg != "" {
			return st.Background(bg)
		}
		return st
	}
	// ── Brand ────────────────────────────────────────────────────────────────
	brand := on(lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)).
		Render(icons.Ghost + " Bubblmail")
	brandW := util.VisibleWidth(icons.Ghost+" Bubblmail") + 1 // +1 leading gutter bar
	brand = on(lipgloss.NewStyle().Foreground(theme.Accent)).Render("▌") + brand

	// ── Right cluster: counts, then sync state ───────────────────────────────
	right, rightW := h.rightCluster(bg)

	// ── Breadcrumb, sized from what the fixed cells leave behind ─────────────
	//
	// Padding: one column after the brand, two before the right cluster.
	const brandGap, rightGap = 2, 2
	crumbBudget := h.width - brandW - brandGap - rightW - rightGap
	if crumbBudget < 0 {
		crumbBudget = 0
	}
	crumb, crumbW := h.breadcrumb(crumbBudget, bg)

	gapW := h.width - brandW - brandGap - crumbW - rightW
	if gapW < 0 {
		gapW = 0
	}

	row := brand +
		components.Fill(brandGap, bg) +
		crumb +
		components.Fill(gapW, bg) +
		right

	// Clamp: a rounding slip here shifts every row below it.
	row = on(lipgloss.NewStyle().MaxWidth(h.width).Width(h.width)).Render(row)

	return row
}

// breadcrumb renders `account › folder`, trimming the account before the
// folder because the folder is the thing that just changed.
func (h *Header) breadcrumb(budget int, bg lipgloss.Color) (string, int) {
	theme := h.theme
	if budget <= 0 {
		return "", 0
	}
	on := func(st lipgloss.Style) lipgloss.Style {
		if bg != "" {
			return st.Background(bg)
		}
		return st
	}

	acct := util.SingleLine(h.activeAccount)
	folder := util.SingleLine(h.activeFolder)
	sep := " " + icons.ChevronRight + " "
	sepW := util.VisibleWidth(sep)

	acctSt := on(lipgloss.NewStyle().Foreground(theme.TextMuted))
	folderSt := on(lipgloss.NewStyle().Foreground(theme.Text).Bold(true))
	sepSt := on(lipgloss.NewStyle().Foreground(theme.Border))

	// The breadcrumb repeats the sidebar's glyph and its colour, so the header
	// and the selected row in the tree are recognisably the same folder.
	markSt := on(lipgloss.NewStyle().Foreground(folderTint(theme, h.activeFolder)))
	mark := ""
	markW := 0
	if folder != "" {
		mark = markSt.Render(folderIcon(h.activeFolder, true)) + on(lipgloss.NewStyle()).Render(" ")
		markW = util.VisibleWidth(folderIcon(h.activeFolder, true)) + 1
	}

	switch {
	case acct != "" && folder != "":
		// Give the folder at least half the budget, then let the account use
		// whatever is left.
		folderW := util.VisibleWidth(folder) + markW
		acctW := util.VisibleWidth(acct)
		if acctW+sepW+folderW > budget {
			maxFolder := budget - sepW - markW - 3 // leave room for a stub account
			if maxFolder < 1 {
				maxFolder = 1
			}
			if folderW-markW > maxFolder {
				folder = util.TruncateText(folder, maxFolder)
				folderW = util.VisibleWidth(folder) + markW
			}
			acctBudget := budget - sepW - folderW
			if acctBudget < 0 {
				acctBudget = 0
			}
			acct = util.TruncateText(acct, acctBudget)
			acctW = util.VisibleWidth(acct)
		}
		if acct == "" {
			return mark + folderSt.Render(folder), folderW
		}
		return acctSt.Render(acct) + sepSt.Render(sep) + mark + folderSt.Render(folder),
			acctW + sepW + folderW
	case folder != "":
		folder = util.TruncateText(folder, budget-markW)
		return mark + folderSt.Render(folder), util.VisibleWidth(folder) + markW
	case acct != "":
		acct = util.TruncateText(acct, budget)
		return acctSt.Render(acct), util.VisibleWidth(acct)
	}
	return "", 0
}

// rightCluster renders the counts and sync state, and reports its width.
func (h *Header) rightCluster(bg lipgloss.Color) (string, int) {
	theme := h.theme
	on := func(st lipgloss.Style) lipgloss.Style {
		if bg != "" {
			return st.Background(bg)
		}
		return st
	}

	var parts []string
	var w int
	add := func(rendered, plain string) {
		if plain == "" {
			return
		}
		if len(parts) > 0 {
			parts = append(parts, on(lipgloss.NewStyle().Foreground(theme.Border)).Render("  ·  "))
			w += 5
		}
		parts = append(parts, rendered)
		w += util.VisibleWidth(plain)
	}

	if h.filterLabel != "" {
		lbl := util.TruncateText(util.SingleLine(h.filterLabel), 24)
		add(on(lipgloss.NewStyle().Foreground(theme.Accent)).Render(icons.Filter+" "+lbl),
			icons.Filter+" "+lbl)
	}

	if h.unread > 0 {
		plain := fmt.Sprintf("%d unread", h.unread)
		add(on(lipgloss.NewStyle().Foreground(theme.Unread).Bold(true)).Render(plain), plain)
	} else if h.total > 0 {
		plain := fmt.Sprintf("%d", h.total)
		add(on(lipgloss.NewStyle().Foreground(theme.TextFaint)).Render(plain), plain)
	}

	switch h.syncState {
	case "syncing":
		plain := icons.Syncing + " syncing"
		add(on(lipgloss.NewStyle().Foreground(theme.Warning)).Render(plain), plain)
	case "synced":
		plain := icons.Synced + " synced"
		add(on(lipgloss.NewStyle().Foreground(theme.Success)).Render(plain), plain)
	case "error":
		plain := icons.Error + " sync error"
		add(on(lipgloss.NewStyle().Foreground(theme.Error)).Render(plain), plain)
	}

	if len(parts) == 0 {
		return "", 0
	}
	out := ""
	for _, p := range parts {
		out += p
	}
	return out, w
}
