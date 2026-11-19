# Conduit

A terminal UI for monitoring [Bitbucket Cloud](https://bitbucket.org/) pipelines. Built with [Bubbletea](https://github.com/charmbracelet/bubbletea).

## Features

- **Dashboard** — Auto-refreshing pipeline list with status, branch, trigger, and duration
- **Detail view** — Step-by-step progress with live log streaming that auto-follows the active step
- **Commit graph** — GitHub-style contribution heatmap of the last year of commits, for one repo or across every active repo in the workspace
- **Repo selector** — Browse workspace repositories with pagination, search, and favorites
- **OAuth** — Client credentials authentication with automatic token caching

## Install

### Homebrew

```bash
brew install lancodev/tap/conduit
```

### Prebuilt binaries

Download a Linux or macOS (amd64/arm64) archive from the
[releases page](https://github.com/lancodev/conduit/releases), extract it, and
put `conduit` somewhere on your `PATH`.

### Arch Linux

An AUR package is planned. Until then, build it with the bundled PKGBUILD:

```bash
git clone https://github.com/lancodev/conduit.git
cd conduit/packaging/arch
makepkg -si
```

### From source

```bash
git clone https://github.com/lancodev/conduit.git
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

### Config file keys

| Key | Description |
|---|---|
| `default_workspace` | Workspace used when none is given |
| `default_repository` | Repository used when none is given |
| `refresh_interval` | Dashboard auto-refresh, in seconds |
| `favorites` | Repos pinned to the selector's Favorites tab |
| `author_emails` | Addresses you commit under, for the workspace commit graph |

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
| `g` | Commit graph |
| `home` | Jump to newest pipeline |
| `r` | Refresh |
| `esc` | Back to repo selector |
| `q` | Quit |

#### Commit graph

| Key | Action |
|---|---|
| `tab` | Switch between this repo and the whole workspace |
| `r` | Refetch / rescan |
| `q` / `esc` | Back to dashboard |

#### Detail view

| Key | Action |
|---|---|
| `tab` | Cycle through steps |
| `up` / `down` | Scroll logs |
| `r` | Refresh |
| `q` / `esc` | Back to dashboard |

## Commit graph

Press `g` from the dashboard to see a GitHub-style contribution heatmap — one
column per week, one cell per day, for the last 365 days, alongside totals for
commits, active days, busiest day, and streaks. Press `tab` to switch between
**this repo** and the **whole workspace**.

Cell intensity uses GitHub's five levels, scaled to each repository's own
quartiles rather than to fixed commit counts, so a quiet repo still shows
contrast instead of a uniformly pale grid. The grid clips to the most recent
weeks that fit your terminal width.

### Whose commits count

Bitbucket's client-credentials OAuth grant has no end-user identity — `/2.0/user`
returns the OAuth consumer, not you — so the workspace graph cannot detect who
you are. Tell it, in `~/.config/conduit/config.yml`:

```yaml
author_emails:
    - you@work.com
    - you@personal.com
```

List every address you commit under; it is common to have more than one. With no
`author_emails` set, the workspace graph shows *every* author's commits and
lists the most active addresses it found, so you can copy the right ones in.

Matching uses the email in the raw git author string. Bitbucket only links a
commit to an account when the git identity matches a Bitbucket user, so commits
made with a misconfigured `git config user.email` cannot be attributed.

### Cost and caching

There is no cross-repository commit API and no server-side author or date
filter, so the workspace graph scans each repository in turn. Two things keep
that affordable:

- **Dormant repos are skipped.** Repos are enumerated newest-first by
  `updated_on` and the walk stops at the cutoff. In a 580-repo workspace that
  typically leaves ~130 to consider.
- **Unchanged repos cost zero requests.** Each repo's commits are cached under
  your user cache directory (`~/.cache/conduit` on Linux, or `$XDG_CACHE_HOME`;
  `~/Library/Caches/conduit` on macOS), keyed by repo UUID. On a
  rescan, any repo whose `updated_on` is unchanged is served from disk.

In practice a first scan of ~128 repos takes about four minutes; a rescan takes
about twenty seconds. The graph fills in progressively as repos complete and is
labelled `Provisional` until the scan finishes.

A scan is capped at 800 requests to stay inside Bitbucket's ~1,000/hour limit
and leave room for the dashboard. If the budget or the rate limit is reached,
the graph renders what it has and says so rather than showing a confidently
wrong total. Repos that are too large to walk fully are flagged the same way,
and are always rescanned rather than cached as if complete.

The workspace graph is never part of the dashboard's auto-refresh — press `r`
to rescan.
