package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	daysPerWeek = 7
	graphDays   = 365
	cellWidth   = 2
	gutterWidth = 4
	cellBlock   = "██"
	dayKeyFmt   = "2006-01-02"
)

// contribGrid is a year of daily commit counts laid out the way GitHub draws
// them: one column per week, one row per weekday with Sunday on top.
type contribGrid struct {
	start  time.Time
	end    time.Time
	weeks  int
	counts map[string]int
	total  int
	active int
	max    int
	levels [4]int
}

// buildContribGrid buckets commits into local calendar days ending at now.
//
// Days are compared at local midnight rather than by subtracting timestamps,
// because a calendar day is 23 or 25 hours across a DST boundary.
func buildContribGrid(commits []Commit, now time.Time) contribGrid {
	end := localMidnight(now)
	firstDay := end.AddDate(0, 0, -(graphDays - 1))
	start := firstDay.AddDate(0, 0, -int(firstDay.Weekday()))

	g := contribGrid{
		start:  start,
		end:    end,
		counts: make(map[string]int),
	}

	days := 0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		days++
	}
	g.weeks = (days + daysPerWeek - 1) / daysPerWeek

	for _, commit := range commits {
		day := localMidnight(commit.Date)
		if day.Before(start) || day.After(end) {
			continue
		}
		g.counts[day.Format(dayKeyFmt)]++
		g.total++
	}

	dailyCounts := make([]int, 0, len(g.counts))
	for _, count := range g.counts {
		dailyCounts = append(dailyCounts, count)
		if count > g.max {
			g.max = count
		}
	}
	g.active = len(dailyCounts)

	sort.Ints(dailyCounts)
	g.levels = quartileThresholds(dailyCounts)

	return g
}

func localMidnight(t time.Time) time.Time {
	local := t.Local()
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
}

// quartileThresholds returns the upper bound of levels 1-3 plus the maximum,
// derived from the quartiles of days that had at least one commit. GitHub
// scales intensity to the repo's own activity rather than to fixed counts, so a
// quiet repo still shows contrast instead of a uniform pale grid.
func quartileThresholds(sortedCounts []int) [4]int {
	if len(sortedCounts) == 0 {
		return [4]int{0, 0, 0, 0}
	}

	at := func(fraction float64) int {
		return sortedCounts[int(fraction*float64(len(sortedCounts)-1))]
	}

	thresholds := [4]int{at(0.25), at(0.50), at(0.75), sortedCounts[len(sortedCounts)-1]}

	// Force strictly increasing bounds so that repos where every quartile lands
	// on the same count still map distinct counts to distinct levels.
	for i := 1; i < len(thresholds); i++ {
		if thresholds[i] <= thresholds[i-1] {
			thresholds[i] = thresholds[i-1] + 1
		}
	}

	return thresholds
}

func (g contribGrid) level(count int) int {
	switch {
	case count <= 0:
		return 0
	case count <= g.levels[0]:
		return 1
	case count <= g.levels[1]:
		return 2
	case count <= g.levels[2]:
		return 3
	default:
		return 4
	}
}

func (g contribGrid) dayAt(week, weekday int) time.Time {
	return g.start.AddDate(0, 0, week*daysPerWeek+weekday)
}

func (g contribGrid) countAt(week, weekday int) (int, bool) {
	day := g.dayAt(week, weekday)
	if day.After(g.end) {
		return 0, false
	}
	return g.counts[day.Format(dayKeyFmt)], true
}

// streaks returns the run of consecutive commit days ending today, and the
// longest such run anywhere in range.
func (g contribGrid) streaks() (current, longest int) {
	run := 0
	for d := g.start; !d.After(g.end); d = d.AddDate(0, 0, 1) {
		if g.counts[d.Format(dayKeyFmt)] > 0 {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	return run, longest
}

// visibleWeeks is how many of the most recent week columns fit in width.
func (g contribGrid) visibleWeeks(width int) int {
	if width <= 0 {
		return g.weeks
	}
	fits := (width - gutterWidth) / cellWidth
	if fits < 1 {
		fits = 1
	}
	if fits > g.weeks {
		return g.weeks
	}
	return fits
}

func (g contribGrid) render(weeks int) string {
	firstWeek := g.weeks - weeks

	var b strings.Builder
	b.WriteString(g.renderMonthLabels(firstWeek, weeks))
	b.WriteString("\n")

	for weekday := 0; weekday < daysPerWeek; weekday++ {
		b.WriteString(dayLabelStyle.Render(weekdayLabel(weekday)))
		for week := firstWeek; week < g.weeks; week++ {
			count, ok := g.countAt(week, weekday)
			if !ok {
				b.WriteString(strings.Repeat(" ", cellWidth))
				continue
			}
			b.WriteString(contribStyles[g.level(count)].Render(cellBlock))
		}
		b.WriteString("\n")
	}

	return b.String()
}

func weekdayLabel(weekday int) string {
	switch weekday {
	case 1:
		return "Mon "
	case 3:
		return "Wed "
	case 5:
		return "Fri "
	default:
		return strings.Repeat(" ", gutterWidth)
	}
}

// renderMonthLabels stamps month abbreviations onto a blank line at the column
// where each month's first week begins, skipping any that would collide with a
// label already placed.
func (g contribGrid) renderMonthLabels(firstWeek, weeks int) string {
	line := []rune(strings.Repeat(" ", weeks*cellWidth))

	for i := 0; i < weeks; i++ {
		week := firstWeek + i
		day := g.dayAt(week, 0)
		if week > 0 && day.Month() == g.dayAt(week-1, 0).Month() {
			continue
		}

		label := []rune(day.Format("Jan"))
		pos := i * cellWidth
		if pos+len(label) > len(line) {
			continue
		}

		collides := false
		for offset := -1; offset < len(label); offset++ {
			at := pos + offset
			if at >= 0 && at < len(line) && line[at] != ' ' {
				collides = true
				break
			}
		}
		if collides {
			continue
		}

		copy(line[pos:], label)
	}

	return strings.Repeat(" ", gutterWidth) + monthLabelStyle.Render(string(line))
}

// renderStats wraps the summary counters to width. Widths must come from
// lipgloss.Width, not len: these strings carry ANSI colour codes, so len
// overstates a 90-column line as 262 bytes and the wrap never triggers.
func (g contribGrid) renderStats(width int) string {
	current, longest := g.streaks()

	stat := func(label string, value int) string {
		return statValueStyle.Render(fmt.Sprintf("%d", value)) + " " + statLabelStyle.Render(label)
	}

	parts := []string{
		stat("commits", g.total),
		stat("active days", g.active),
		stat("busiest day", g.max),
		stat("day streak", current),
		stat("longest streak", longest),
	}

	separator := statLabelStyle.Render("  •  ")
	separatorWidth := lipgloss.Width(separator)
	indent := strings.Repeat(" ", gutterWidth)

	var lines []string
	line, lineWidth := indent, gutterWidth

	for i, part := range parts {
		partWidth := lipgloss.Width(part)
		if i > 0 {
			if width > 0 && lineWidth+separatorWidth+partWidth > width {
				lines = append(lines, line)
				line, lineWidth = indent, gutterWidth
			} else {
				line += separator
				lineWidth += separatorWidth
			}
		}
		line += part
		lineWidth += partWidth
	}

	return strings.Join(append(lines, line), "\n")
}

func (g contribGrid) renderLegend() string {
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", gutterWidth))
	b.WriteString(helpStyle.Render("Less "))
	for level := 0; level < len(contribStyles); level++ {
		b.WriteString(contribStyles[level].Render(cellBlock))
	}
	b.WriteString(helpStyle.Render(" More"))
	return b.String()
}

// graphViewRender renders the commit activity heatmap, either for the current
// repo or aggregated across the workspace.
func (m model) graphViewRender() string {
	if m.graphMode == 1 {
		return m.workspaceGraphRender()
	}
	return m.repoGraphRender()
}

func (m model) graphTabs() string {
	repoLabel, wsLabel := "This repo", "Whole workspace"
	if m.graphMode == 0 {
		return "  " + activeTabStyle.Render(repoLabel) + " │ " + inactiveTabStyle.Render(wsLabel)
	}
	return "  " + inactiveTabStyle.Render(repoLabel) + " │ " + activeTabStyle.Render(wsLabel)
}

func (m model) repoGraphRender() string {
	var b strings.Builder

	repo := repoStyle.Render(m.cfg.DefaultWorkspace + "/" + m.cfg.DefaultRepository)
	title := headerStyle.Render("conduit") + "  " + repo + "  " + timestampStyle.Render("commit activity")
	if m.graphLoading {
		title += "  " + m.spinner.View()
	}
	b.WriteString(title + "\n")
	b.WriteString(m.graphTabs() + "\n\n")

	if m.errMsg != "" {
		b.WriteString(errorStyle.Render("Error: "+m.errMsg) + "\n\n")
	}

	switch {
	case !m.graphLoaded && m.graphLoading:
		b.WriteString("  " + m.spinner.View() + " Loading commit history...\n\n")

	case !m.graphLoaded:
		b.WriteString(helpStyle.Render("  No commit history loaded. Press r to retry.") + "\n\n")

	case m.graph.total == 0:
		b.WriteString(helpStyle.Render("  No commits in the last year.") + "\n\n")

	default:
		b.WriteString(m.graph.render(m.graph.visibleWeeks(m.width)))
		b.WriteString("\n")
		b.WriteString(m.graph.renderLegend())
		b.WriteString("\n\n")
		b.WriteString(m.graph.renderStats(m.width))
		b.WriteString("\n")
		if m.graphTruncated {
			b.WriteString(warnStyle.Render(
				"    Partial: history limit reached, older commits not counted.") + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("tab switch • r refresh • esc/q back"))

	return b.String()
}

func (m model) workspaceGraphRender() string {
	var b strings.Builder

	scope := repoStyle.Render(m.cfg.DefaultWorkspace)
	title := headerStyle.Render("conduit") + "  " + scope + "  " + timestampStyle.Render(m.workspaceScopeLabel())
	if m.wsScanning {
		title += "  " + m.spinner.View()
	}
	b.WriteString(title + "\n")
	b.WriteString(m.graphTabs() + "\n\n")

	if m.wsErr != "" {
		b.WriteString(errorStyle.Render("Error: "+m.wsErr) + "\n\n")
	}

	if len(m.cfg.AuthorEmails) == 0 {
		b.WriteString(warnStyle.Render("  No author_emails configured — showing every author's commits.") + "\n")
		b.WriteString(helpStyle.Render("  Add author_emails to ~/.config/conduit/config.yml to see only your own.") + "\n\n")
	}

	switch {
	case !m.wsLoaded && m.wsScanning:
		b.WriteString("  " + m.spinner.View() + " Scanning workspace...\n\n")

	case !m.wsLoaded:
		b.WriteString(helpStyle.Render("  No workspace history loaded. Press r to scan.") + "\n\n")

	case m.wsGraph.total == 0:
		b.WriteString(helpStyle.Render("  No matching commits in the last year.") + "\n")
		b.WriteString(m.renderAuthorHint())
		b.WriteString("\n")

	default:
		b.WriteString(m.wsGraph.render(m.wsGraph.visibleWeeks(m.width)))
		b.WriteString("\n")
		b.WriteString(m.wsGraph.renderLegend())
		b.WriteString("\n\n")
		b.WriteString(m.wsGraph.renderStats(m.width))
		b.WriteString("\n")
		b.WriteString(m.renderWorkspaceWarnings())
	}

	b.WriteString("\n" + helpStyle.Render("tab switch • r rescan • esc/q back"))

	return b.String()
}

func (m model) workspaceScopeLabel() string {
	if m.wsScanning && m.wsTotal > 0 {
		return fmt.Sprintf("scanning %d/%d repos", m.wsDone, m.wsTotal)
	}
	if m.wsTotal > 0 {
		return fmt.Sprintf("%d repos", m.wsTotal)
	}
	return "workspace activity"
}

// renderWorkspaceWarnings labels every way the aggregate can be incomplete.
// An under-counted heatmap looks confidently correct, so silence is the one
// unacceptable option here.
func (m model) renderWorkspaceWarnings() string {
	var b strings.Builder

	if m.wsScanning {
		b.WriteString(warnStyle.Render(fmt.Sprintf(
			"    Provisional: %d of %d repos scanned.", m.wsDone, m.wsTotal)) + "\n")
	}
	if m.wsPartial {
		msg := "    Partial: request budget reached, some repos not scanned."
		if m.wsRetryAfter > 0 {
			msg = fmt.Sprintf("    Partial: rate limited, retry in %s.", m.wsRetryAfter.Round(time.Second))
		}
		b.WriteString(warnStyle.Render(msg) + "\n")
	}
	if m.wsTruncated {
		b.WriteString(warnStyle.Render(
			"    Partial: some repos were too large to scan fully.") + "\n")
	}

	return b.String()
}

// renderAuthorHint lists the most active authors actually present in the cache
// so a user whose author_emails don't match can see what to put there.
func (m model) renderAuthorHint() string {
	if len(m.wsCaches) == 0 {
		return ""
	}
	emails := knownAuthorEmails(m.wsCaches, 5)
	if len(emails) == 0 {
		return ""
	}
	return helpStyle.Render("  Most active authors seen: "+strings.Join(emails, ", ")) + "\n"
}
