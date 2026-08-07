package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// cacheSchema is bumped whenever the on-disk shape or its meaning changes.
// A mismatch is treated as a miss and refetched; caches are never migrated,
// because a wrong migration produces a plausible-looking but incorrect graph.
const cacheSchema = 1

// cachedCommit is the minimal record the heatmap needs. Messages, display
// names and nicknames are discarded: across ~127 repos of history they dwarf
// the useful data and are never rendered.
type cachedCommit struct {
	Date      time.Time `json:"d"`
	AuthorRaw string    `json:"a"`
}

// repoCache is one repository's scan, stored as a whole. It is only ever
// replaced wholesale, never patched, which is what makes rescan-on-change
// immune to the merge, force-push and branch-deletion hazards that break
// incremental hash-walking.
type repoCache struct {
	Schema    int            `json:"schema"`
	UUID      string         `json:"uuid"`
	FullName  string         `json:"full_name"`
	UpdatedOn string         `json:"updated_on"`
	ScannedAt time.Time      `json:"scanned_at"`
	Truncated bool           `json:"truncated"`
	Commits   []cachedCommit `json:"commits"`
}

func cacheDir(workspace string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locating cache dir: %w", err)
	}
	return filepath.Join(base, "conduit", "commits", sanitizeFilename(workspace)), nil
}

// sanitizeFilename keeps workspace and repo UUIDs from escaping the cache
// directory via separators or traversal sequences in an API-supplied value.
func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// loadRepoCaches reads every cached repo for a workspace, keyed by repo UUID.
// Unreadable or stale-schema entries are skipped rather than failing the load;
// they simply become cache misses and get rescanned.
func loadRepoCaches(workspace string) (map[string]repoCache, error) {
	dir, err := cacheDir(workspace)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return map[string]repoCache{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading cache dir: %w", err)
	}

	caches := make(map[string]repoCache, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var rc repoCache
		if err := json.Unmarshal(data, &rc); err != nil || rc.Schema != cacheSchema || rc.UUID == "" {
			continue
		}
		caches[rc.UUID] = rc
	}

	return caches, nil
}

// saveRepoCache writes one repo's cache atomically, so an interrupted write
// can never leave a half-written file that would later parse as a short but
// valid commit list.
func saveRepoCache(workspace string, rc repoCache) error {
	dir, err := cacheDir(workspace)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}

	rc.Schema = cacheSchema
	data, err := json.Marshal(rc)
	if err != nil {
		return fmt.Errorf("encoding cache: %w", err)
	}

	final := filepath.Join(dir, sanitizeFilename(rc.UUID)+".json")
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp cache file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("writing cache: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("securing cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing cache: %w", err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replacing cache: %w", err)
	}

	return nil
}

// pruneRepoCaches deletes caches for repos no longer in the active set, so a
// deleted or newly-dormant repo stops contributing to the totals.
func pruneRepoCaches(workspace string, keep map[string]bool) {
	dir, err := cacheDir(workspace)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		if !keep[strings.TrimSuffix(name, ".json")] {
			os.Remove(filepath.Join(dir, name))
		}
	}
}

var authorEmailPattern = regexp.MustCompile(`<([^>]+)>`)

// authorEmail extracts the address from a git author string such as
// "Jane Doe <jane@example.com>". Bitbucket only guarantees this raw form:
// author.user is null whenever the git identity isn't linked to an account,
// so matching by parsed email is the only option that works for every commit.
func authorEmail(raw string) string {
	if match := authorEmailPattern.FindStringSubmatch(raw); len(match) == 2 {
		return strings.ToLower(strings.TrimSpace(match[1]))
	}
	return strings.ToLower(strings.TrimSpace(raw))
}

// commitsMatchingAuthors flattens caches into commits authored by the given
// emails. An empty set means "no filter", which renders whole-workspace
// activity instead of one person's.
func commitsMatchingAuthors(caches map[string]repoCache, emails map[string]bool) []Commit {
	var commits []Commit
	for _, rc := range caches {
		for _, cc := range rc.Commits {
			if emails != nil && !emails[authorEmail(cc.AuthorRaw)] {
				continue
			}
			commits = append(commits, Commit{
				Date:   cc.Date,
				Author: CommitAuthor{Raw: cc.AuthorRaw},
			})
		}
	}
	return commits
}

// knownAuthorEmails lists every author seen in the cache, most frequent first,
// so the UI can tell a user what to put in author_emails when they have not
// configured it or configured it wrongly.
func knownAuthorEmails(caches map[string]repoCache, limit int) []string {
	counts := map[string]int{}
	for _, rc := range caches {
		for _, cc := range rc.Commits {
			counts[authorEmail(cc.AuthorRaw)]++
		}
	}

	emails := make([]string, 0, len(counts))
	for email := range counts {
		emails = append(emails, email)
	}
	sortByCountDesc(emails, counts)

	if len(emails) > limit {
		emails = emails[:limit]
	}
	return emails
}

func sortByCountDesc(items []string, counts map[string]int) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0; j-- {
			if counts[items[j]] > counts[items[j-1]] {
				items[j], items[j-1] = items[j-1], items[j]
				continue
			}
			break
		}
	}
}
