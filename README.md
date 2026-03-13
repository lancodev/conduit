# Conduit

A terminal UI for monitoring [Bitbucket Cloud](https://bitbucket.org/) pipelines. Built with [Bubbletea](https://github.com/charmbracelet/bubbletea).

## Features

- **Dashboard** — Auto-refreshing pipeline list with status, branch, trigger, and duration
- **Detail view** — Step-by-step progress with live log streaming that auto-follows the active step
- **Repo selector** — Browse workspace repositories with pagination, search, and favorites
- **OAuth** — Client credentials authentication with automatic token caching

## Install

### Homebrew

```bash
brew install kylekanderson/tap/conduit
```

### From source

```bash
git clone https://github.com/kylekanderson/conduit.git
cd conduit
go build -o conduit .
```

## Setup

Run the interactive auth setup to configure your Bitbucket OAuth credentials:

```bash
conduit auth
```

This creates a config file at `~/.config/conduit/config.yml`. You'll need a Bitbucket OAuth consumer with **Client Credentials** grant type — create one at **Bitbucket > Workspace settings > OAuth consumers**.

### Environment variables

These override the config file:

| Variable | Description |
|---|---|
| `CONDUIT_CLIENT_ID` | Bitbucket OAuth Client ID |
| `CONDUIT_CLIENT_SECRET` | Bitbucket OAuth Client Secret |
| `CONDUIT_WORKSPACE` | Default workspace |
| `CONDUIT_REPOSITORY` | Default repository |

## Usage

```
conduit                       # Dashboard for default/favorite repo
conduit <build_number>        # Detail view for a specific build
conduit -r workspace/repo     # Dashboard for a specific repo
conduit -i <seconds>          # Custom refresh interval (default: 10)
conduit auth                  # Set up credentials
```

### Keybindings

#### Repo selector

| Key | Action |
|---|---|
| `j` / `k` | Navigate list |
| `tab` | Switch between Favorites and Browse tabs |
| `enter` | Select repo |
| `f` | Toggle favorite (persisted to config) |
| `/` | Search repositories |
| `h` / `l` | Previous / next page |
| `q` | Quit |

#### Dashboard

| Key | Action |
|---|---|
| `j` / `k` | Navigate pipelines |
| `enter` | View pipeline detail |
| `r` | Refresh |
| `esc` | Back to repo selector |
| `q` | Quit |

#### Detail view

| Key | Action |
|---|---|
| `tab` | Cycle through steps |
| `up` / `down` | Scroll logs |
| `r` | Refresh |
| `q` / `esc` | Back to dashboard |
