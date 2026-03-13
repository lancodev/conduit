package main

import (
	"fmt"
	"strings"
)

// detailViewRender renders the detail view for a single pipeline.
func (m model) detailViewRender() string {
	var b strings.Builder

	// Header.
	title := headerStyle.Render(fmt.Sprintf("conduit #%d", m.buildNumber))
	if m.loading {
		title += "  " + m.spinner.View()
	}
	b.WriteString(title + "\n")

	// Error.
	if m.errMsg != "" {
		b.WriteString(errorStyle.Render("Error: "+m.errMsg) + "\n\n")
	}

	// Pipeline metadata.
	if m.pipeline != nil {
		b.WriteString(m.renderPipelineMeta())
		b.WriteString("\n")
	}

	// Steps list.
	if len(m.steps) > 0 {
		b.WriteString(headerStyle.Render("Steps") + "\n")
		b.WriteString(m.renderSteps())
		b.WriteString("\n")
	}

	// Log viewport.
	if m.logContent != "" || m.activeStepUUID() != "" {
		stepName := "logs"
		if m.selectedStep >= 0 && m.selectedStep < len(m.steps) {
			stepName = m.steps[m.selectedStep].Name
		}
		b.WriteString(headerStyle.Render(fmt.Sprintf("Log: %s", stepName)) + "\n")
		b.WriteString(logBorderStyle.Render(m.logViewport.View()))
		b.WriteString("\n")
		scrollPct := int(m.logViewport.ScrollPercent() * 100)
		b.WriteString(timestampStyle.Render(fmt.Sprintf("  %d%%", scrollPct)))
		b.WriteString("\n")
	}

	// Help.
	help := "↑/↓ scroll • tab cycle steps • r refresh"
	if m.directDetail {
		help += " • q quit"
	} else {
		help += " • q/esc back"
	}
	b.WriteString("\n" + helpStyle.Render(help))

	return b.String()
}

// renderPipelineMeta renders pipeline metadata as key-value pairs.
func (m model) renderPipelineMeta() string {
	p := m.pipeline
	var b strings.Builder

	icon, style := pipelineStatusInfo(*p)
	statusText := style.Render(icon + " " + pipelineStatusText(*p))

	meta := []struct {
		label string
		value string
	}{
		{"Status", statusText},
		{"Branch", p.Target.RefName},
		{"Commit", shortHash(p.Target.Commit.Hash)},
		{"Creator", creatorName(p)},
		{"Trigger", p.Trigger.Name},
		{"Created", formatTime(p.CreatedOn)},
		{"Duration", formatDuration(p.Duration)},
	}

	for _, m := range meta {
		b.WriteString(metaLabelStyle.Render(m.label) + metaValueStyle.Render(m.value) + "\n")
	}

	return b.String()
}

// renderSteps renders the step list with status icons.
func (m model) renderSteps() string {
	var b strings.Builder

	for i, step := range m.steps {
		result := ""
		if step.State.Result != nil {
			result = step.State.Result.Name
		}
		icon, style := statusIcon(step.State.Name, result)

		line := fmt.Sprintf("%s %s", icon, step.Name)
		if step.Duration > 0 {
			line += timestampStyle.Render(fmt.Sprintf("  %s", formatDuration(step.Duration)))
		}

		if i == m.selectedStep {
			b.WriteString(selectedStepStyle.Render(style.Render(line)) + "\n")
		} else {
			b.WriteString(stepStyle.Render(style.Render(line)) + "\n")
		}
	}

	return b.String()
}

func shortHash(hash string) string {
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
}

func creatorName(p *Pipeline) string {
	if p.Creator.DisplayName != "" {
		return p.Creator.DisplayName
	}
	if p.Creator.Name != "" {
		return p.Creator.Name
	}
	return "—"
}
