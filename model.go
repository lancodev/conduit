package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// viewState tracks which view is active.
type viewState int

const (
	repoSelectView viewState = iota
	dashboardView
	detailView
)

// model is the main Bubbletea model.
type model struct {
	// View state.
	state viewState

	// Config & API.
	cfg    Config
	api    *APIClient
	width  int
	height int

	// Dashboard state.
	pipelines   []Pipeline
	dashTable   table.Model
	lastRefresh time.Time

	// Detail state.
	buildNumber      int
	pipeline         *Pipeline
	steps            []Step
	selectedStep     int
	userSelectedStep bool // true when user manually tabbed to a step; disables auto-follow
	logContent       string
	logOffset        int // byte offset for incremental log fetching
	logViewport      viewport.Model

	// Repo selector state.
	repos           []Repository
	repoPage        int
	repoTotalPages  int
	repoSearchQuery string
	repoSearchMode  bool
	repoTab         int // 0=favorites, 1=browse
	repoLoading     bool
	repoSelected    int

	// Shared.
	spinner spinner.Model
	loading bool
	errMsg  string

	// Launch flags.
	directDetail bool
	repoFromFlag bool
}

// --- Messages ---

type tickMsg time.Time

type pipelineListMsg struct {
	pipelines []Pipeline
	err       error
}

type pipelineDetailMsg struct {
	pipeline *Pipeline
	steps    []Step
	err      error
}

type stepLogMsg struct {
	content   string
	newOffset int
	err       error
}

type windowSizeMsg struct {
	width  int
	height int
}

type repoListMsg struct {
	repos      []Repository
	totalCount int
	err        error
}

// --- Commands ---

func tickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func fetchPipelinesCmd(api *APIClient, workspace, repo string) tea.Cmd {
	return func() tea.Msg {
		pipelines, err := api.FetchPipelines(workspace, repo)
		return pipelineListMsg{pipelines: pipelines, err: err}
	}
}

func fetchDetailCmd(api *APIClient, workspace, repo string, buildNum int) tea.Cmd {
	return func() tea.Msg {
		pipeline, pErr := api.FetchPipeline(workspace, repo, buildNum)
		steps, sErr := api.FetchSteps(workspace, repo, buildNum)
		if pErr != nil {
			return pipelineDetailMsg{err: pErr}
		}
		if sErr != nil {
			return pipelineDetailMsg{pipeline: pipeline, err: sErr}
		}
		return pipelineDetailMsg{pipeline: pipeline, steps: steps}
	}
}

func fetchLogCmd(api *APIClient, workspace, repo string, buildNum int, stepUUID string, offset int) tea.Cmd {
	return func() tea.Msg {
		content, newOffset, err := api.FetchStepLog(workspace, repo, buildNum, stepUUID, offset)
		return stepLogMsg{content: content, newOffset: newOffset, err: err}
	}
}

func fetchReposCmd(api *APIClient, workspace string, page int) tea.Cmd {
	return func() tea.Msg {
		repos, total, err := api.FetchRepositories(workspace, page)
		return repoListMsg{repos: repos, totalCount: total, err: err}
	}
}

func searchReposCmd(api *APIClient, workspace, query string) tea.Cmd {
	return func() tea.Msg {
		repos, err := api.SearchRepositories(workspace, query)
		return repoListMsg{repos: repos, totalCount: len(repos), err: err}
	}
}

// --- Init ---

func initialModel(cfg Config, buildNumber int, directDetail bool, repoFromFlag bool) model {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))

	m := model{
		cfg:          cfg,
		api:          NewAPIClient(cfg.Auth.ClientID, cfg.Auth.ClientSecret),
		spinner:      s,
		loading:      true,
		buildNumber:  buildNumber,
		directDetail: directDetail,
		repoFromFlag: repoFromFlag,
		repoPage:     1,
		logViewport:  viewport.New(80, 20),
	}

	switch {
	case buildNumber > 0:
		m.state = detailView
	case repoFromFlag:
		m.state = dashboardView
		m.dashTable = newDashboardTable()
	case len(cfg.Favorites) >= 2:
		m.state = repoSelectView
		m.repoTab = 0
	case len(cfg.Favorites) == 1:
		parts := strings.SplitN(cfg.Favorites[0], "/", 2)
		if len(parts) == 2 {
			m.cfg.DefaultWorkspace = parts[0]
			m.cfg.DefaultRepository = parts[1]
		}
		m.state = dashboardView
		m.dashTable = newDashboardTable()
	case cfg.DefaultRepository != "":
		m.state = dashboardView
		m.dashTable = newDashboardTable()
	default:
		m.state = repoSelectView
		m.repoTab = 1
	}

	return m
}

func (m model) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, m.spinner.Tick)

	switch m.state {
	case detailView:
		cmds = append(cmds, fetchDetailCmd(m.api, m.cfg.DefaultWorkspace, m.cfg.DefaultRepository, m.buildNumber))
	case dashboardView:
		cmds = append(cmds, fetchPipelinesCmd(m.api, m.cfg.DefaultWorkspace, m.cfg.DefaultRepository))
	case repoSelectView:
		if m.repoTab == 1 {
			m.repoLoading = true
			cmds = append(cmds, fetchReposCmd(m.api, m.cfg.DefaultWorkspace, m.repoPage))
		}
	}

	cmds = append(cmds, tickCmd(time.Duration(m.cfg.RefreshInterval)*time.Second))
	return tea.Batch(cmds...)
}

// --- Update ---

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.dashTable.SetWidth(msg.Width)
		m.dashTable.SetHeight(msg.Height - 6) // room for header/footer
		m.logViewport.Width = msg.Width - 4
		m.logViewport.Height = msg.Height - 16 // room for header/steps/footer
		if m.logViewport.Height < 3 {
			m.logViewport.Height = 3
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tickMsg:
		cmds = append(cmds, tickCmd(time.Duration(m.cfg.RefreshInterval)*time.Second))
		switch m.state {
		case dashboardView:
			cmds = append(cmds, fetchPipelinesCmd(m.api, m.cfg.DefaultWorkspace, m.cfg.DefaultRepository))
		case detailView:
			cmds = append(cmds, fetchDetailCmd(m.api, m.cfg.DefaultWorkspace, m.cfg.DefaultRepository, m.buildNumber))
			if m.activeStepUUID() != "" {
				cmds = append(cmds, fetchLogCmd(m.api, m.cfg.DefaultWorkspace, m.cfg.DefaultRepository, m.buildNumber, m.activeStepUUID(), m.logOffset))
			}
		}
		return m, tea.Batch(cmds...)

	case pipelineListMsg:
		m.loading = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.errMsg = ""
			m.pipelines = msg.pipelines
			m.lastRefresh = time.Now()
			m.dashTable.SetRows(pipelineRows(msg.pipelines))
		}
		return m, nil

	case pipelineDetailMsg:
		m.loading = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.errMsg = ""
			m.pipeline = msg.pipeline
			m.steps = msg.steps
			// Auto-follow the active step unless the user manually tabbed.
			if !m.userSelectedStep {
				newStep := autoSelectStep(msg.steps)
				if newStep != m.selectedStep {
					m.selectedStep = newStep
					m.logContent = ""
					m.logOffset = 0
					m.logViewport.SetContent("")
				}
			}
			// Fetch log for active step.
			uuid := m.activeStepUUID()
			if uuid != "" {
				cmds = append(cmds, fetchLogCmd(m.api, m.cfg.DefaultWorkspace, m.cfg.DefaultRepository, m.buildNumber, uuid, m.logOffset))
			}
		}
		return m, tea.Batch(cmds...)

	case repoListMsg:
		m.repoLoading = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.errMsg = ""
			m.repos = msg.repos
			m.repoSelected = 0
			if msg.totalCount > 0 {
				m.repoTotalPages = (msg.totalCount + 24) / 25
			} else {
				m.repoTotalPages = 1
			}
		}
		return m, nil

	case stepLogMsg:
		if msg.err != nil {
			// Don't overwrite main error for log failures.
		} else if msg.content != "" {
			m.logContent += msg.content
			m.logOffset = msg.newOffset
			m.logViewport.SetContent(m.logContent)
			m.logViewport.GotoBottom()
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.state == repoSelectView {
		return m.handleRepoSelectKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit

	case "q":
		if m.state == detailView && !m.directDetail {
			return m.switchToDashboard()
		}
		return m, tea.Quit

	case "esc":
		if m.state == detailView && !m.directDetail {
			return m.switchToDashboard()
		}
		if m.state == dashboardView && !m.repoFromFlag {
			return m.switchToRepoSelect()
		}
		return m, tea.Quit

	case "backspace":
		if m.state == dashboardView && !m.repoFromFlag {
			return m.switchToRepoSelect()
		}

	case "r":
		if m.state == dashboardView {
			m.loading = true
			return m, fetchPipelinesCmd(m.api, m.cfg.DefaultWorkspace, m.cfg.DefaultRepository)
		}
		if m.state == detailView {
			m.loading = true
			return m, fetchDetailCmd(m.api, m.cfg.DefaultWorkspace, m.cfg.DefaultRepository, m.buildNumber)
		}

	case "enter":
		if m.state == dashboardView {
			return m.switchToDetail()
		}

	case "tab":
		if m.state == detailView && len(m.steps) > 0 {
			m.userSelectedStep = true
			m.selectedStep = (m.selectedStep + 1) % len(m.steps)
			m.logContent = ""
			m.logOffset = 0
			m.logViewport.SetContent("")
			uuid := m.activeStepUUID()
			if uuid != "" {
				return m, fetchLogCmd(m.api, m.cfg.DefaultWorkspace, m.cfg.DefaultRepository, m.buildNumber, uuid, 0)
			}
		}
	}

	// Pass key messages to sub-components.
	if m.state == dashboardView {
		var cmd tea.Cmd
		m.dashTable, cmd = m.dashTable.Update(msg)
		return m, cmd
	}

	if m.state == detailView {
		var cmd tea.Cmd
		m.logViewport, cmd = m.logViewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m model) switchToDetail() (tea.Model, tea.Cmd) {
	row := m.dashTable.SelectedRow()
	if row == nil {
		return m, nil
	}

	// Build number is in first column.
	buildNum := 0
	fmt.Sscanf(row[0], "#%d", &buildNum)
	if buildNum == 0 {
		return m, nil
	}

	m.state = detailView
	m.buildNumber = buildNum
	m.pipeline = nil
	m.steps = nil
	m.selectedStep = 0
	m.userSelectedStep = false
	m.logContent = ""
	m.logOffset = 0
	m.logViewport.SetContent("")
	m.loading = true
	m.errMsg = ""

	return m, fetchDetailCmd(m.api, m.cfg.DefaultWorkspace, m.cfg.DefaultRepository, buildNum)
}

func (m model) switchToDashboard() (tea.Model, tea.Cmd) {
	m.state = dashboardView
	m.pipeline = nil
	m.steps = nil
	m.logContent = ""
	m.logOffset = 0
	m.loading = true
	m.errMsg = ""

	return m, fetchPipelinesCmd(m.api, m.cfg.DefaultWorkspace, m.cfg.DefaultRepository)
}

func (m model) switchToRepoSelect() (tea.Model, tea.Cmd) {
	m.state = repoSelectView
	m.pipeline = nil
	m.steps = nil
	m.logContent = ""
	m.logOffset = 0
	m.repoSelected = 0
	m.errMsg = ""
	if m.repoTab == 0 {
		return m, nil
	}
	m.repoLoading = true
	return m, fetchReposCmd(m.api, m.cfg.DefaultWorkspace, m.repoPage)
}

func (m model) activeStepUUID() string {
	if m.selectedStep >= 0 && m.selectedStep < len(m.steps) {
		return m.steps[m.selectedStep].UUID
	}
	return ""
}

// autoSelectStep picks the first in-progress step, or the last completed one.
func autoSelectStep(steps []Step) int {
	for i, s := range steps {
		if s.State.Name == "IN_PROGRESS" {
			return i
		}
	}
	// Fall back to last step.
	if len(steps) > 0 {
		return len(steps) - 1
	}
	return 0
}

// --- View ---

func (m model) View() string {
	switch m.state {
	case repoSelectView:
		return m.repoSelectViewRender()
	case detailView:
		return m.detailViewRender()
	default:
		return m.dashboardViewRender()
	}
}
