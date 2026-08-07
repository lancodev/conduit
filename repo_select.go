package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m model) repoSelectViewRender() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("conduit") + "\n\n")

	// Tab bar.
	var favLabel, browseLabel string
	if m.repoTab == 0 {
		favLabel = activeTabStyle.Render("Favorites")
		browseLabel = inactiveTabStyle.Render("Browse All")
	} else {
		favLabel = inactiveTabStyle.Render("Favorites")
		browseLabel = activeTabStyle.Render("Browse All")
	}
	b.WriteString("  " + favLabel + " │ " + browseLabel + "\n\n")

	// List content.
	if m.repoTab == 0 {
		if len(m.cfg.Favorites) == 0 {
			b.WriteString(helpStyle.Render("  No favorites yet. Press tab to browse repos, then f to favorite.") + "\n")
		}
		for i, fav := range m.cfg.Favorites {
			if i == m.repoSelected {
				b.WriteString(repoCursorStyle.Render(iconArrowRight) + " " + starStyle.Render("★") + " " + fav + "\n")
			} else {
				b.WriteString("  " + starStyle.Render("★") + " " + fav + "\n")
			}
		}
	} else {
		if m.repoLoading && len(m.repos) == 0 {
			b.WriteString("  " + m.spinner.View() + " Loading...\n")
		}
		for i, repo := range m.repos {
			star := "  "
			if m.isFavorite(repo.FullName) {
				star = " " + starStyle.Render("★")
			}
			if i == m.repoSelected {
				b.WriteString(repoCursorStyle.Render(iconArrowRight) + " " + repo.FullName + star + "\n")
			} else {
				b.WriteString("  " + repo.FullName + star + "\n")
			}
		}
		if len(m.repos) > 0 && m.repoTotalPages > 0 {
			b.WriteString(fmt.Sprintf("\n  Page %d/%d\n", m.repoPage, m.repoTotalPages))
		}
	}

	b.WriteString("\n")

	if m.errMsg != "" {
		b.WriteString(errorStyle.Render("  Error: "+m.errMsg) + "\n\n")
	}

	if m.repoSearchMode {
		b.WriteString("  Search: " + m.repoSearchQuery + "_\n\n")
	}

	b.WriteString(helpStyle.Render("/ search • ←/→ pages • tab switch • f favorite • enter select • q quit"))

	return b.String()
}

func (m model) handleRepoSelectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.repoSearchMode {
		key := msg.String()
		switch key {
		case "enter":
			m.repoSearchMode = false
			if m.repoSearchQuery != "" {
				m.repoLoading = true
				m.repoPage = 1
				return m, searchReposCmd(m.api, m.cfg.DefaultWorkspace, m.repoSearchQuery)
			}
			return m, nil
		case "esc":
			m.repoSearchMode = false
			m.repoSearchQuery = ""
			return m, nil
		case "backspace":
			if len(m.repoSearchQuery) > 0 {
				m.repoSearchQuery = m.repoSearchQuery[:len(m.repoSearchQuery)-1]
			}
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		default:
			if len(key) == 1 {
				m.repoSearchQuery += key
			}
			return m, nil
		}
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab":
		m.repoTab = (m.repoTab + 1) % 2
		m.repoSelected = 0
		if m.repoTab == 1 && len(m.repos) == 0 {
			m.repoLoading = true
			return m, fetchReposCmd(m.api, m.cfg.DefaultWorkspace, m.repoPage)
		}
		return m, nil

	case "enter":
		return m.selectRepoFromList()

	case "f":
		return m.toggleFavorite()

	case "j", "down":
		maxIdx := m.repoListLen() - 1
		if m.repoSelected < maxIdx {
			m.repoSelected++
		}
		return m, nil

	case "k", "up":
		if m.repoSelected > 0 {
			m.repoSelected--
		}
		return m, nil

	case "l", "right":
		if m.repoTab == 1 && m.repoPage < m.repoTotalPages {
			m.repoPage++
			m.repoSelected = 0
			m.repoLoading = true
			return m, fetchReposCmd(m.api, m.cfg.DefaultWorkspace, m.repoPage)
		}
		return m, nil

	case "h", "left":
		if m.repoTab == 1 && m.repoPage > 1 {
			m.repoPage--
			m.repoSelected = 0
			m.repoLoading = true
			return m, fetchReposCmd(m.api, m.cfg.DefaultWorkspace, m.repoPage)
		}
		return m, nil

	case "/":
		if m.repoTab == 1 {
			m.repoSearchMode = true
			m.repoSearchQuery = ""
		}
		return m, nil

	case "r":
		if m.repoTab == 1 {
			m.repoLoading = true
			return m, fetchReposCmd(m.api, m.cfg.DefaultWorkspace, m.repoPage)
		}
		return m, nil
	}

	return m, nil
}

func (m model) selectRepoFromList() (tea.Model, tea.Cmd) {
	var fullName string
	if m.repoTab == 0 {
		if m.repoSelected >= 0 && m.repoSelected < len(m.cfg.Favorites) {
			fullName = m.cfg.Favorites[m.repoSelected]
		}
	} else {
		if m.repoSelected >= 0 && m.repoSelected < len(m.repos) {
			fullName = m.repos[m.repoSelected].FullName
		}
	}

	if fullName == "" {
		return m, nil
	}

	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return m, nil
	}

	m.cfg.DefaultWorkspace = parts[0]
	m.cfg.DefaultRepository = parts[1]
	m.state = dashboardView
	m.dashTable = newDashboardTable()
	m.loading = true
	m.errMsg = ""
	m.graph = contribGrid{}
	m.graphLoaded = false

	if m.width > 0 {
		m.dashTable.SetWidth(m.width)
		m.dashTable.SetHeight(m.height - 6)
	}

	return m, fetchPipelinesCmd(m.api, parts[0], parts[1])
}

func (m model) toggleFavorite() (tea.Model, tea.Cmd) {
	var fullName string
	if m.repoTab == 0 {
		if m.repoSelected >= 0 && m.repoSelected < len(m.cfg.Favorites) {
			fullName = m.cfg.Favorites[m.repoSelected]
		}
	} else {
		if m.repoSelected >= 0 && m.repoSelected < len(m.repos) {
			fullName = m.repos[m.repoSelected].FullName
		}
	}

	if fullName == "" {
		return m, nil
	}

	idx := -1
	for i, fav := range m.cfg.Favorites {
		if fav == fullName {
			idx = i
			break
		}
	}

	if idx >= 0 {
		m.cfg.Favorites = append(m.cfg.Favorites[:idx], m.cfg.Favorites[idx+1:]...)
		if m.repoTab == 0 && m.repoSelected >= len(m.cfg.Favorites) {
			m.repoSelected = len(m.cfg.Favorites) - 1
			if m.repoSelected < 0 {
				m.repoSelected = 0
			}
		}
	} else {
		m.cfg.Favorites = append(m.cfg.Favorites, fullName)
	}

	_ = SaveConfig(m.cfg)
	return m, nil
}

func (m model) isFavorite(fullName string) bool {
	for _, fav := range m.cfg.Favorites {
		if fav == fullName {
			return true
		}
	}
	return false
}

func (m model) repoListLen() int {
	if m.repoTab == 0 {
		return len(m.cfg.Favorites)
	}
	return len(m.repos)
}
