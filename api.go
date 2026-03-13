package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const bitbucketAPI = "https://api.bitbucket.org/2.0"

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

// doGet performs an authenticated GET request and returns the response body.
func (c *APIClient) doGet(endpoint string) ([]byte, error) {
	if err := c.ensureToken(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	token := c.accessToken
	c.mu.Unlock()

	req, err := http.NewRequest("GET", endpoint, nil)
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(data))
	}

	return data, nil
}

// --- JSON response types ---

// Repository represents a Bitbucket repository.
type Repository struct {
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
