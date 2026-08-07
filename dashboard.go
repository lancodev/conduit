package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

// newDashboardTable creates the initial table for the dashboard view.
func newDashboardTable() table.Model {
	columns := []table.Column{
		{Title: "Build", Width: 8},
		{Title: "Status", Width: 16},
		{Title: "Branch", Width: 26},
		{Title: "Trigger", Width: 10},
		{Title: "Created", Width: 18},
		{Title: "Duration", Width: 10},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(12),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(true)
	t.SetStyles(s)

	return t
}

// pipelineRows converts pipelines to table rows.
func pipelineRows(pipelines []Pipeline) []table.Row {
	rows := make([]table.Row, 0, len(pipelines))
	for _, p := range pipelines {
		// Plain ASCII only — Unicode icons and ANSI codes break the bubbles
		// table width calculation, causing column misalignment.
		status := pipelineStatusLabel(p)
		rows = append(rows, table.Row{
			fmt.Sprintf("#%d", p.BuildNumber),
			status,
			truncate(p.Target.RefName, 24),
			p.Trigger.Name,
			formatTime(p.CreatedOn),
			formatDuration(p.Duration),
		})
	}
	return rows
}

// pipelineStatusInfo returns icon and style for a pipeline.
func pipelineStatusInfo(p Pipeline) (string, lipgloss.Style) {
	result := ""
	if p.State.Result != nil {
		result = p.State.Result.Name
	}
	return statusIcon(p.State.Name, result)
}

// pipelineStatusText returns a short status label.
func pipelineStatusText(p Pipeline) string {
	switch p.State.Name {
	case "COMPLETED":
		if p.State.Result != nil {
			return strings.ToLower(p.State.Result.Name)
		}
		return "completed"
	case "IN_PROGRESS":
		return "running"
	case "PENDING":
		return "pending"
	default:
		return strings.ToLower(p.State.Name)
	}
}

// pipelineStatusLabel returns an ASCII-safe status label for the table.
func pipelineStatusLabel(p Pipeline) string {
	switch p.State.Name {
	case "COMPLETED":
		if p.State.Result != nil {
			switch p.State.Result.Name {
			case "SUCCESSFUL":
				return "[ok] successful"
			case "FAILED":
				return "[!!] failed"
			case "ERROR":
				return "[!!] error"
			case "STOPPED":
				return "[--] stopped"
			default:
				return "[--] " + strings.ToLower(p.State.Result.Name)
			}
		}
		return "[--] completed"
	case "IN_PROGRESS":
		return "[>>] running"
	case "PENDING":
		return "[..] pending"
	default:
		return "[  ] " + strings.ToLower(p.State.Name)
	}
}

// dashboardViewRender renders the dashboard view.
func (m model) dashboardViewRender() string {
	var b strings.Builder

	// Header.
	repo := repoStyle.Render(m.cfg.DefaultWorkspace + "/" + m.cfg.DefaultRepository)
	title := headerStyle.Render("conduit") + "  " + repo
	if !m.lastRefresh.IsZero() {
		title += "  " + timestampStyle.Render("updated "+m.lastRefresh.Format("15:04:05"))
	}
	if m.loading {
		title += "  " + m.spinner.View()
	}
	b.WriteString(title + "\n\n")

	// Error message.
	if m.errMsg != "" {
		b.WriteString(errorStyle.Render("Error: "+m.errMsg) + "\n\n")
	}

	// Table.
	b.WriteString(m.dashTable.View())
	b.WriteString("\n\n")

	// Help.
	help := helpStyle.Render("↑/↓/j/k navigate • enter view • g graph • r refresh • esc back • q quit")
	b.WriteString(help)

	return b.String()
}

// --- Helpers ---

func formatTime(iso string) string {
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05.000000+00:00", iso)
		if err != nil {
			return iso
		}
	}
	return t.Local().Format("Jan 02 15:04")
}

func formatDuration(seconds int) string {
	if seconds <= 0 {
		return "—"
	}
	d := time.Duration(seconds) * time.Second
	if d >= time.Hour {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
