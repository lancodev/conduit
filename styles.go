package main

import "github.com/charmbracelet/lipgloss"

// Colors.
var (
	colorGreen   = lipgloss.Color("#04B575")
	colorRed     = lipgloss.Color("#FF4672")
	colorYellow  = lipgloss.Color("#FDFF90")
	colorBlue    = lipgloss.Color("#7DC4E4")
	colorDim     = lipgloss.Color("#626262")
	colorSubtle  = lipgloss.Color("#888888")
	colorWhite   = lipgloss.Color("#FAFAFA")
	colorMagenta = lipgloss.Color("#F5BDE6")
)

// Contribution heatmap colors, from GitHub's dark theme. Level 0 is lightened
// from GitHub's #161B22 so empty cells stay visible against a dark terminal.
var (
	colorContrib0 = lipgloss.Color("#2D333B")
	colorContrib1 = lipgloss.Color("#0E4429")
	colorContrib2 = lipgloss.Color("#006D32")
	colorContrib3 = lipgloss.Color("#26A641")
	colorContrib4 = lipgloss.Color("#39D353")
)

// Status icons.
const (
	iconSuccess    = "✔"
	iconFailed     = "✖"
	iconRunning    = "●"
	iconPending    = "○"
	iconStopped    = "■"
	iconNotRun     = "–"
	iconArrowRight = "→"
)

// Header and layout styles.
var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBlue).
			MarginBottom(1)

	repoStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorMagenta)

	timestampStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			Italic(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorSubtle)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)
)

// Status styles.
var (
	statusSuccess = lipgloss.NewStyle().Foreground(colorGreen)
	statusFailed  = lipgloss.NewStyle().Foreground(colorRed)
	statusRunning = lipgloss.NewStyle().Foreground(colorYellow)
	statusPending = lipgloss.NewStyle().Foreground(colorDim)
	statusStopped = lipgloss.NewStyle().Foreground(colorDim)
)

// Detail view styles.
var (
	metaLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBlue).
			Width(12)

	metaValueStyle = lipgloss.NewStyle().
			Foreground(colorWhite)

	stepStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	selectedStepStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Bold(true).
				Foreground(colorBlue).
				SetString(iconArrowRight + " ")

	logBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			Padding(0, 1)
)

// Repo selector styles.
var (
	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBlue).
			Underline(true)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(colorSubtle)

	starStyle = lipgloss.NewStyle().
			Foreground(colorYellow)

	repoCursorStyle = lipgloss.NewStyle().
			Foreground(colorBlue).
			Bold(true)
)

// Commit graph styles.
var (
	contribStyles = [5]lipgloss.Style{
		lipgloss.NewStyle().Foreground(colorContrib0),
		lipgloss.NewStyle().Foreground(colorContrib1),
		lipgloss.NewStyle().Foreground(colorContrib2),
		lipgloss.NewStyle().Foreground(colorContrib3),
		lipgloss.NewStyle().Foreground(colorContrib4),
	}

	monthLabelStyle = lipgloss.NewStyle().
			Foreground(colorSubtle)

	dayLabelStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	statLabelStyle = lipgloss.NewStyle().
			Foreground(colorSubtle)

	statValueStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite)

	warnStyle = lipgloss.NewStyle().
			Foreground(colorYellow)
)

// statusIcon returns the icon and style for a given pipeline/step state.
func statusIcon(state, result string) (string, lipgloss.Style) {
	switch state {
	case "COMPLETED":
		switch result {
		case "SUCCESSFUL":
			return iconSuccess, statusSuccess
		case "FAILED", "ERROR":
			return iconFailed, statusFailed
		case "STOPPED":
			return iconStopped, statusStopped
		default:
			return iconStopped, statusPending
		}
	case "IN_PROGRESS":
		return iconRunning, statusRunning
	case "PENDING":
		return iconPending, statusPending
	case "NOT_RUN":
		return iconNotRun, statusPending
	default:
		return iconPending, statusPending
	}
}
