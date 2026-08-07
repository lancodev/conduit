package main

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	scanWorkers = 4
	// scanRequestBudget leaves headroom under Bitbucket's ~1000/hour repository
	// limit so a full workspace scan cannot starve the dashboard's own polling.
	scanRequestBudget = 800
)

// scanEvent reports progress from the workspace scan. Repo-completion events
// carry the whole repo's cache, because a partially-applied repo would leave
// the aggregate internally inconsistent.
type scanEvent struct {
	Repo      string
	Cache     *repoCache
	Done      int
	Total     int
	Skipped   bool
	Truncated bool
	Err       error

	Finished    bool
	BudgetSpent bool
	RetryAfter  time.Duration
}

// governor bounds how many requests a scan may spend and halts every worker at
// once when Bitbucket rate limits, so workers cannot independently back off and
// turn one 429 into a retry storm.
type governor struct {
	mu        sync.Mutex
	remaining int
	limited   bool
	retry     time.Duration
}

func newGovernor(budget int) *governor {
	return &governor{remaining: budget}
}

// reserve claims budget for one repository scan, returning how many requests it
// may spend. Zero means the scan should stop.
func (g *governor) reserve(n int) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.limited || g.remaining <= 0 {
		return 0
	}
	if n > g.remaining {
		n = g.remaining
	}
	g.remaining -= n
	return n
}

func (g *governor) refund(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.remaining += n
}

func (g *governor) rateLimited(retry time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.limited = true
	if retry > g.retry {
		g.retry = retry
	}
}

func (g *governor) state() (limited bool, retry time.Duration, remaining int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.limited, g.retry, g.remaining
}

// scanWorkspace walks every repository active since cutoff, reusing cached
// results for any repo whose updated_on is unchanged. Events stream on the
// returned channel, which closes once the scan finishes.
//
// Repos are scanned concurrently but each repo's pages are fetched serially:
// Bitbucket's commit pagination is an opaque "next" cursor, so page N+1 is not
// addressable until page N returns.
func scanWorkspace(ctx context.Context, api *APIClient, workspace string, cutoff time.Time) <-chan scanEvent {
	events := make(chan scanEvent, scanWorkers*2)

	go func() {
		defer close(events)

		caches, err := loadRepoCaches(workspace)
		if err != nil {
			caches = map[string]repoCache{}
		}

		repos, err := api.FetchActiveRepositories(ctx, workspace, cutoff)
		if err != nil {
			events <- scanEvent{Err: err, Finished: true}
			return
		}

		keep := make(map[string]bool, len(repos))
		for _, repo := range repos {
			keep[sanitizeFilename(repo.UUID)] = true
		}
		pruneRepoCaches(workspace, keep)

		gov := newGovernor(scanRequestBudget)
		total := len(repos)

		var (
			wg       sync.WaitGroup
			mu       sync.Mutex
			done     int
			jobs     = make(chan Repository)
			budgeted bool
		)

		for i := 0; i < scanWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for repo := range jobs {
					event := scanRepo(ctx, api, gov, workspace, repo, caches[repo.UUID], cutoff)

					mu.Lock()
					done++
					event.Done = done
					event.Total = total
					if event.BudgetSpent {
						budgeted = true
					}
					mu.Unlock()

					select {
					case events <- event:
					case <-ctx.Done():
						return
					}
				}
			}()
		}

	dispatch:
		for _, repo := range repos {
			select {
			case jobs <- repo:
			case <-ctx.Done():
				break dispatch
			}
		}
		close(jobs)
		wg.Wait()

		limited, retry, _ := gov.state()
		events <- scanEvent{
			Finished:    true,
			Total:       total,
			Done:        done,
			BudgetSpent: budgeted || limited,
			RetryAfter:  retry,
		}
	}()

	return events
}

// scanRepo returns cached data untouched when the repo has not been updated
// since the last successful scan. That is the whole efficiency story: an
// unchanged repo costs zero requests.
func scanRepo(
	ctx context.Context,
	api *APIClient,
	gov *governor,
	workspace string,
	repo Repository,
	cached repoCache,
	cutoff time.Time,
) scanEvent {
	// A truncated cache is never treated as up to date, or an incomplete walk
	// would be reused forever on the grounds that the repo hasn't changed.
	if cached.UUID != "" && !cached.Truncated && cached.UpdatedOn == repo.UpdatedOn {
		reused := cached
		return scanEvent{Repo: repo.FullName, Cache: &reused, Skipped: true}
	}

	// Walk in chunks, topping the budget up between them, so one very large
	// repo cannot reserve the whole workspace's allowance up front.
	var (
		commits   []Commit
		resume    string
		truncated bool
	)
	for {
		allowance := gov.reserve(commitChunkPages)
		if allowance == 0 {
			truncated = resume != "" || len(commits) == 0
			break
		}

		scan, err := api.FetchCommits(ctx, workspace, repo.Slug, cutoff, resume, allowance)
		gov.refund(allowance - scan.Requests)
		commits = append(commits, scan.Commits...)

		if err != nil {
			var limit *rateLimitError
			if errors.As(err, &limit) {
				gov.rateLimited(limit.RetryAfter)
			}
			// Keep the previous complete cache rather than replacing a repo's
			// history with a partial walk.
			event := scanEvent{Repo: repo.FullName, Err: err, BudgetSpent: true}
			if cached.UUID != "" {
				reused := cached
				event.Cache = &reused
			}
			return event
		}

		resume = scan.Next
		if resume == "" {
			break
		}
	}

	if truncated && cached.UUID != "" {
		// An incomplete walk is worse than yesterday's complete one.
		reused := cached
		return scanEvent{Repo: repo.FullName, Cache: &reused, BudgetSpent: true}
	}

	fresh := repoCache{
		UUID:      repo.UUID,
		FullName:  repo.FullName,
		UpdatedOn: repo.UpdatedOn,
		ScannedAt: time.Now(),
		Truncated: truncated,
		Commits:   make([]cachedCommit, 0, len(commits)),
	}
	for _, commit := range commits {
		fresh.Commits = append(fresh.Commits, cachedCommit{
			Date:      commit.Date,
			AuthorRaw: commit.Author.Raw,
		})
	}

	if err := saveRepoCache(workspace, fresh); err != nil {
		return scanEvent{Repo: repo.FullName, Cache: &fresh, Err: err, Truncated: truncated}
	}

	return scanEvent{Repo: repo.FullName, Cache: &fresh, Truncated: truncated, BudgetSpent: truncated}
}
