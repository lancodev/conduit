package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const bitbucketAPI = "https://api.bitbucket.org/2.0"

const (
	commitPageLen = 100
	// commitChunkPages is how much history a caller walks per budget
	// reservation. Small chunks let a workspace scan share its request budget
	// fairly instead of one enormous repo reserving it all up front.
	commitChunkPages = 20
	// singleRepoMaxPages bounds the one-repo graph, which has no shared budget
	// to answer to. At 100 commits per page this covers 200k commits a year.
	singleRepoMaxPages = 2000
)

// APIClient manages Bitbucket API calls and OAuth tokens.
type APIClient struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

// NewAPIClient creates a new Bitbucket API client.
func NewAPIClient(clientID, clientSecret string) *APIClient {
	return &APIClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// tokenResponse represents the OAuth token response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// ensureToken fetches or refreshes the OAuth token if needed.
func (c *APIClient) ensureToken() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Token still valid (with 60s buffer).
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		return nil
	}

	if c.clientID == "" || c.clientSecret == "" {
		return fmt.Errorf("no credentials configured — run 'conduit auth' to set up")
	}

	creds := base64.StdEncoding.EncodeToString(
		[]byte(c.clientID + ":" + c.clientSecret),
	)

	body := strings.NewReader("grant_type=client_credentials")
	req, err := http.NewRequest("POST", "https://bitbucket.org/site/oauth2/access_token", body)
	if err != nil {
		return fmt.Errorf("creating token request: %w", err)
	}

	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token request returned %d: %s", resp.StatusCode, string(respBody))
	}

	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return fmt.Errorf("decoding token response: %w", err)
	}

	c.accessToken = tok.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	return nil
}

// rateLimitError reports an HTTP 429 so callers can pause rather than retry.
type rateLimitError struct {
	RetryAfter time.Duration
}

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("rate limited by Bitbucket, retry in %s", e.RetryAfter)
}

// doGet performs an authenticated GET request and returns the response body.
func (c *APIClient) doGet(endpoint string) ([]byte, error) {
	return c.doGetCtx(context.Background(), endpoint)
}

// doGetCtx is doGet with cancellation, translating 429 into a rateLimitError
// so a caller fanning out across many repos can stop instead of retrying into
// a deeper rate-limit hole.
func (c *APIClient) doGetCtx(ctx context.Context, endpoint string) ([]byte, error) {
	if err := c.ensureToken(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	token := c.accessToken
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &rateLimitError{RetryAfter: retryAfter(resp.Header)}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(data))
	}

	return data, nil
}

func retryAfter(h http.Header) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return 60 * time.Second
}

// --- JSON response types ---

// Repository represents a Bitbucket repository.
type Repository struct {
	UUID      string            `json:"uuid"`
	Slug      string            `json:"slug"`
	Name      string            `json:"name"`
	FullName  string            `json:"full_name"`
	IsPrivate bool              `json:"is_private"`
	UpdatedOn string            `json:"updated_on"`
	Project   RepositoryProject `json:"project"`
}

// RepositoryProject holds project info for a repository.
type RepositoryProject struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// RepositoryListResponse is the API response for listing repositories.
type RepositoryListResponse struct {
	Values []Repository `json:"values"`
	Page   int          `json:"page"`
	Size   int          `json:"size"`
	Next   string       `json:"next"`
}

// PipelineListResponse is the API response for listing pipelines.
type PipelineListResponse struct {
	Values []Pipeline `json:"values"`
	Page   int        `json:"page"`
}

// Pipeline represents a Bitbucket pipeline.
type Pipeline struct {
	BuildNumber int            `json:"build_number"`
	State       PipelineState  `json:"state"`
	Target      PipelineTarget `json:"target"`
	Trigger     PipelineName   `json:"trigger"`
	Creator     PipelineName   `json:"creator"`
	CreatedOn   string         `json:"created_on"`
	CompletedOn string         `json:"completed_on"`
	Duration    int            `json:"duration_in_seconds"`
}

// PipelineState holds state and optional result.
type PipelineState struct {
	Name   string        `json:"name"`
	Result *PipelineName `json:"result,omitempty"`
}

// PipelineTarget holds ref and commit info.
type PipelineTarget struct {
	RefName string         `json:"ref_name"`
	Commit  PipelineCommit `json:"commit"`
}

// PipelineCommit holds a commit hash.
type PipelineCommit struct {
	Hash string `json:"hash"`
}

// PipelineName is a generic name-holder used for trigger, creator, result.
type PipelineName struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
}

// Commit represents a Bitbucket commit.
type Commit struct {
	Hash    string       `json:"hash"`
	Date    time.Time    `json:"date"`
	Message string       `json:"message"`
	Author  CommitAuthor `json:"author"`
}

// CommitAuthor holds commit authorship. User is nil when the git identity
// doesn't map to a Bitbucket account, so Raw is the reliable fallback.
type CommitAuthor struct {
	Raw  string         `json:"raw"`
	User *BitbucketUser `json:"user,omitempty"`
}

// BitbucketUser holds the account linked to a commit author.
type BitbucketUser struct {
	DisplayName string `json:"display_name"`
	Nickname    string `json:"nickname"`
}

// CommitListResponse is the API response for listing commits. The commits
// endpoint uses iterator pagination: only Next is meaningful, there is no
// reliable page/size to compute a total from.
type CommitListResponse struct {
	Values  []Commit `json:"values"`
	Next    string   `json:"next"`
	Pagelen int      `json:"pagelen"`
}

// Step represents a pipeline step.
type Step struct {
	UUID     string        `json:"uuid"`
	Name     string        `json:"name"`
	State    PipelineState `json:"state"`
	Duration int           `json:"duration_in_seconds"`
}

// StepListResponse is the API response for listing steps.
type StepListResponse struct {
	Values []Step `json:"values"`
}

// --- API methods ---

func pipelinesURL(workspace, repo string) string {
	return fmt.Sprintf("%s/repositories/%s/%s/pipelines?sort=-created_on&pagelen=10",
		bitbucketAPI, workspace, repo)
}

func pipelineURL(workspace, repo string, buildNum int) string {
	return fmt.Sprintf("%s/repositories/%s/%s/pipelines/%d",
		bitbucketAPI, workspace, repo, buildNum)
}

func stepsURL(workspace, repo string, buildNum int) string {
	return fmt.Sprintf("%s/repositories/%s/%s/pipelines/%d/steps/",
		bitbucketAPI, workspace, repo, buildNum)
}

func stepLogURL(workspace, repo string, buildNum int, stepUUID string) string {
	encoded := url.PathEscape(stepUUID)
	return fmt.Sprintf("%s/repositories/%s/%s/pipelines/%d/steps/%s/log",
		bitbucketAPI, workspace, repo, buildNum, encoded)
}

// commitsURL requests only the fields the heatmap needs. Commit messages
// dominate the payload and are never displayed, so dropping them keeps a
// 127-repo scan's parsing and bandwidth cost down.
func commitsURL(workspace, repo string) string {
	return fmt.Sprintf("%s/repositories/%s/%s/commits?pagelen=%d&fields=next,values.hash,values.date,values.author.raw",
		bitbucketAPI, workspace, repo, commitPageLen)
}

// FetchPipelines fetches the most recent pipelines.
func (c *APIClient) FetchPipelines(workspace, repo string) ([]Pipeline, error) {
	data, err := c.doGet(pipelinesURL(workspace, repo))
	if err != nil {
		return nil, err
	}

	var resp PipelineListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decoding pipelines: %w", err)
	}

	return resp.Values, nil
}

// FetchPipeline fetches a single pipeline by build number.
func (c *APIClient) FetchPipeline(workspace, repo string, buildNum int) (*Pipeline, error) {
	data, err := c.doGet(pipelineURL(workspace, repo, buildNum))
	if err != nil {
		return nil, err
	}

	var p Pipeline
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("decoding pipeline: %w", err)
	}

	return &p, nil
}

// FetchSteps fetches steps for a pipeline.
func (c *APIClient) FetchSteps(workspace, repo string, buildNum int) ([]Step, error) {
	data, err := c.doGet(stepsURL(workspace, repo, buildNum))
	if err != nil {
		return nil, err
	}

	var resp StepListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decoding steps: %w", err)
	}

	return resp.Values, nil
}

// CommitScan is the result of walking part of one repository's history.
//
// Next is non-empty when the page limit stopped the walk with history still
// unread; pass it back as resume to continue. A caller that abandons a
// non-empty Next has an incomplete window and must say so, because a heatmap
// missing commits looks confidently wrong rather than obviously broken.
type CommitScan struct {
	Commits  []Commit
	Requests int
	Next     string
}

// FetchCommits fetches commits made on or after cutoff, across all branches.
//
// Bitbucket paginates this endpoint with an opaque "next" URL rather than page
// numbers, and offers no server-side date filter, so the cutoff is applied here
// while walking. Commits arrive newest-first but merges can interleave slightly
// older ones, so the walk only stops once an entire page predates the cutoff
// rather than on the first old commit.
func (c *APIClient) FetchCommits(
	ctx context.Context,
	workspace, repo string,
	cutoff time.Time,
	resume string,
	maxPages int,
) (CommitScan, error) {
	var scan CommitScan

	endpoint := resume
	if endpoint == "" {
		endpoint = commitsURL(workspace, repo)
	}

	for endpoint != "" {
		if scan.Requests >= maxPages {
			scan.Next = endpoint
			break
		}

		data, err := c.doGetCtx(ctx, endpoint)
		scan.Requests++
		if err != nil {
			return scan, err
		}

		var resp CommitListResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return scan, fmt.Errorf("decoding commits: %w", err)
		}

		if len(resp.Values) == 0 {
			break
		}

		inRange := 0
		for _, commit := range resp.Values {
			if commit.Date.Before(cutoff) {
				continue
			}
			scan.Commits = append(scan.Commits, commit)
			inRange++
		}

		if inRange == 0 {
			break
		}

		endpoint = resp.Next
	}

	return scan, nil
}

// FetchActiveRepositories returns repos touched on or after cutoff.
//
// Results are sorted by updated_on descending so the walk can stop at the first
// stale repo instead of paging the whole workspace. Any commit bumps
// updated_on, so this prunes dormant repos without ever dropping one that has
// commits in the window.
func (c *APIClient) FetchActiveRepositories(ctx context.Context, workspace string, cutoff time.Time) ([]Repository, error) {
	endpoint := fmt.Sprintf(
		"%s/repositories/%s?pagelen=100&sort=-updated_on&fields=next,values.uuid,values.full_name,values.slug,values.updated_on",
		bitbucketAPI, workspace)

	var active []Repository

	for endpoint != "" {
		data, err := c.doGetCtx(ctx, endpoint)
		if err != nil {
			return nil, err
		}

		var resp RepositoryListResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, fmt.Errorf("decoding repositories: %w", err)
		}

		if len(resp.Values) == 0 {
			break
		}

		for _, repo := range resp.Values {
			updated, err := time.Parse(time.RFC3339Nano, repo.UpdatedOn)
			if err != nil || updated.Before(cutoff) {
				return active, nil
			}
			active = append(active, repo)
		}

		endpoint = resp.Next
	}

	return active, nil
}

// FetchRepositories fetches repositories for a workspace, paginated.
func (c *APIClient) FetchRepositories(workspace string, page int) ([]Repository, int, error) {
	endpoint := fmt.Sprintf("%s/repositories/%s?pagelen=25&page=%d&sort=-updated_on",
		bitbucketAPI, workspace, page)

	data, err := c.doGet(endpoint)
	if err != nil {
		return nil, 0, err
	}

	var resp RepositoryListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, 0, fmt.Errorf("decoding repositories: %w", err)
	}

	return resp.Values, resp.Size, nil
}

// SearchRepositories searches repos by name within a workspace.
func (c *APIClient) SearchRepositories(workspace, query string) ([]Repository, error) {
	q := fmt.Sprintf(`name~"%s"`, query)
	endpoint := fmt.Sprintf("%s/repositories/%s?pagelen=25&q=%s&sort=-updated_on",
		bitbucketAPI, workspace, url.QueryEscape(q))

	data, err := c.doGet(endpoint)
	if err != nil {
		return nil, err
	}

	var resp RepositoryListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decoding repositories: %w", err)
	}

	return resp.Values, nil
}

// FetchStepLog fetches log content for a step, starting from byteOffset.
// Returns the log text and the new byte offset.
func (c *APIClient) FetchStepLog(workspace, repo string, buildNum int, stepUUID string, byteOffset int) (string, int, error) {
	if err := c.ensureToken(); err != nil {
		return "", byteOffset, err
	}

	c.mu.Lock()
	token := c.accessToken
	c.mu.Unlock()

	logURL := stepLogURL(workspace, repo, buildNum, stepUUID)

	req, err := http.NewRequest("GET", logURL, nil)
	if err != nil {
		return "", byteOffset, fmt.Errorf("creating log request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	if byteOffset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", byteOffset))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", byteOffset, fmt.Errorf("log request failed: %w", err)
	}
	defer resp.Body.Close()

	// 416 = range not satisfiable (no new content).
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return "", byteOffset, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", byteOffset, fmt.Errorf("log request returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", byteOffset, fmt.Errorf("reading log: %w", err)
	}

	newOffset := byteOffset + len(data)
	return string(data), newOffset, nil
}
