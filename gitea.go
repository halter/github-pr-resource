package resource

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"code.gitea.io/sdk/gitea"
	"github.com/shurcooL/githubv4"
)

// GiteaClient implements the Github interface against a Gitea instance.
type GiteaClient struct {
	Client     *gitea.Client
	Owner      string
	Repository string
	Source     *Source
}

// NewGiteaClient ...
func NewGiteaClient(s *Source) (*GiteaClient, error) {
	owner, repository, err := parseRepository(s.GiteaRepository)
	if err != nil {
		return nil, err
	}

	opts := []gitea.ClientOption{gitea.SetToken(s.GiteaAccessToken)}
	if s.SkipSSLVerification {
		opts = append(opts, gitea.SetHTTPClient(&http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}))
	}

	client, err := gitea.NewClient(s.GiteaEndpoint, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create gitea client: %w", err)
	}

	return &GiteaClient{
		Client:     client,
		Owner:      owner,
		Repository: repository,
		Source:     s,
	}, nil
}

func giteaPullState(p *gitea.PullRequest) githubv4.PullRequestState {
	if p.HasMerged {
		return githubv4.PullRequestStateMerged
	}
	if p.State == gitea.StateClosed {
		return githubv4.PullRequestStateClosed
	}
	return githubv4.PullRequestStateOpen
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// ListPullRequests gets the last commit on all pull requests with the matching state.
func (m *GiteaClient) ListPullRequests(prStates []githubv4.PullRequestState) ([]*PullRequest, error) {
	wanted := make(map[githubv4.PullRequestState]bool, len(prStates))
	for _, s := range prStates {
		wanted[s] = true
	}

	listState := gitea.StateAll
	switch {
	case wanted[githubv4.PullRequestStateOpen] && !wanted[githubv4.PullRequestStateClosed] && !wanted[githubv4.PullRequestStateMerged]:
		listState = gitea.StateOpen
	case !wanted[githubv4.PullRequestStateOpen]:
		listState = gitea.StateClosed
	}

	var response []*PullRequest
	page := 1
	for {
		pulls, resp, err := m.Client.ListRepoPullRequests(m.Owner, m.Repository, gitea.ListPullRequestsOptions{
			ListOptions: gitea.ListOptions{Page: page, PageSize: 50},
			State:       listState,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list gitea pull requests: %w", err)
		}
		if len(pulls) == 0 {
			break
		}
		for _, p := range pulls {
			if !wanted[giteaPullState(p)] {
				continue
			}
			pull, err := m.newPullRequest(p, "")
			if err != nil {
				return nil, err
			}
			if len(m.Source.StatusFilters) > 0 {
				contexts, err := m.listCommitStatuses(pull.Tip.OID)
				if err != nil {
					return nil, err
				}
				pull.Tip.Status.Contexts = contexts
			}
			if m.Source.RequiredReviewApprovals > 0 {
				count, err := m.approvedReviewCount(p.Index)
				if err != nil {
					return nil, err
				}
				pull.ApprovedReviewCount = count
			}
			response = append(response, pull)
		}
		if resp != nil && resp.NextPage > 0 {
			page = resp.NextPage
		} else {
			page++
		}
	}
	return response, nil
}

func (m *GiteaClient) newPullRequest(p *gitea.PullRequest, commitRef string) (*PullRequest, error) {
	sha := commitRef
	if sha == "" && p.Head != nil {
		sha = p.Head.Sha
	}
	tip, err := m.getCommit(sha)
	if err != nil {
		return nil, err
	}

	var labels []LabelObject
	for _, l := range p.Labels {
		labels = append(labels, LabelObject{Name: l.Name})
	}

	obj := PullRequestObject{
		ID:        strconv.FormatInt(p.ID, 10),
		Number:    int(p.Index),
		Title:     p.Title,
		URL:       p.HTMLURL,
		IsDraft:   p.Draft,
		State:     giteaPullState(p),
		CreatedAt: githubv4.DateTime{Time: derefTime(p.Created)},
		ClosedAt:  githubv4.DateTime{Time: derefTime(p.Closed)},
		MergedAt:  githubv4.DateTime{Time: derefTime(p.Merged)},
	}
	if p.Base != nil {
		obj.BaseRefName = p.Base.Ref
		if p.Base.Repository != nil {
			obj.Repository.URL = p.Base.Repository.CloneURL
		}
	}
	if obj.Repository.URL == "" {
		obj.Repository.URL = fmt.Sprintf("%s/%s/%s.git", strings.TrimSuffix(m.Source.GiteaEndpoint, "/"), m.Owner, m.Repository)
	}
	if p.Head != nil {
		obj.HeadRefName = p.Head.Ref
		obj.IsCrossRepository = p.Base != nil && p.Head.RepoID != p.Base.RepoID
	}

	return &PullRequest{
		PullRequestObject: obj,
		Tip:               tip,
		Labels:            labels,
		Provider:          ProviderGitea,
	}, nil
}

func (m *GiteaClient) getCommit(sha string) (CommitObject, error) {
	commit, _, err := m.Client.GetSingleCommit(m.Owner, m.Repository, sha)
	if err != nil {
		return CommitObject{}, fmt.Errorf("failed to get gitea commit '%s': %w", sha, err)
	}

	tip := CommitObject{ID: sha, OID: sha}
	if commit.CommitMeta != nil {
		tip.OID = commit.SHA
		tip.CommittedDate = githubv4.DateTime{Time: commit.Created}
	}
	if commit.RepoCommit != nil {
		tip.Message = commit.RepoCommit.Message
		if commit.RepoCommit.Committer != nil {
			if t, err := time.Parse(time.RFC3339, commit.RepoCommit.Committer.Date); err == nil {
				tip.CommittedDate = githubv4.DateTime{Time: t}
			}
		}
		if commit.RepoCommit.Author != nil {
			tip.Author.Email = commit.RepoCommit.Author.Email
			tip.Author.User.Login = commit.RepoCommit.Author.Name
		}
	}
	if commit.Author != nil {
		tip.Author.User.Login = commit.Author.UserName
	}
	return tip, nil
}

func (m *GiteaClient) listCommitStatuses(sha string) ([]StatusContext, error) {
	combined, _, err := m.Client.GetCombinedStatus(m.Owner, m.Repository, sha)
	if err != nil {
		return nil, fmt.Errorf("failed to get gitea commit statuses: %w", err)
	}
	var contexts []StatusContext
	for _, s := range combined.Statuses {
		contexts = append(contexts, StatusContext{
			Context:   s.Context,
			State:     strings.ToUpper(string(s.State)),
			CreatedAt: githubv4.DateTime{Time: s.Created},
		})
	}
	return contexts, nil
}

func (m *GiteaClient) approvedReviewCount(index int64) (int, error) {
	count := 0
	page := 1
	for {
		reviews, resp, err := m.Client.ListPullReviews(m.Owner, m.Repository, index, gitea.ListPullReviewsOptions{
			ListOptions: gitea.ListOptions{Page: page, PageSize: 50},
		})
		if err != nil {
			return 0, fmt.Errorf("failed to list gitea pull reviews: %w", err)
		}
		if len(reviews) == 0 {
			break
		}
		for _, r := range reviews {
			if r.State == gitea.ReviewStateApproved && !r.Dismissed {
				count++
			}
		}
		if resp != nil && resp.NextPage > 0 {
			page = resp.NextPage
		} else {
			page++
		}
	}
	return count, nil
}

// ListModifiedFiles in a pull request.
func (m *GiteaClient) ListModifiedFiles(prNumber int) ([]string, error) {
	var files []string
	page := 1
	for {
		result, resp, err := m.Client.ListPullRequestFiles(m.Owner, m.Repository, int64(prNumber), gitea.ListPullRequestFilesOptions{
			ListOptions: gitea.ListOptions{Page: page, PageSize: 50},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list gitea pull request files: %w", err)
		}
		if len(result) == 0 {
			break
		}
		for _, f := range result {
			files = append(files, f.Filename)
		}
		if resp != nil && resp.NextPage > 0 {
			page = resp.NextPage
		} else {
			page++
		}
	}
	return files, nil
}

// PostComment to a pull request or issue.
func (m *GiteaClient) PostComment(prNumber, comment string) error {
	pr, err := strconv.ParseInt(prNumber, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to convert pull request number to int: %w", err)
	}
	_, _, err = m.Client.CreateIssueComment(m.Owner, m.Repository, pr, gitea.CreateIssueCommentOption{
		Body: comment,
	})
	return err
}

// GetPullRequest ...
func (m *GiteaClient) GetPullRequest(prNumber, commitRef string) (*PullRequest, error) {
	pr, err := strconv.ParseInt(prNumber, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to convert pull request number to int: %w", err)
	}
	pull, _, err := m.Client.GetPullRequest(m.Owner, m.Repository, pr)
	if err != nil {
		return nil, fmt.Errorf("failed to get gitea pull request: %w", err)
	}
	return m.newPullRequest(pull, commitRef)
}

// GetChangedFiles ...
func (m *GiteaClient) GetChangedFiles(prNumber string, commitRef string) ([]ChangedFileObject, error) {
	pr, err := strconv.Atoi(prNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to convert pull request number to int: %w", err)
	}
	files, err := m.ListModifiedFiles(pr)
	if err != nil {
		return nil, err
	}
	var cfo []ChangedFileObject
	for _, f := range files {
		cfo = append(cfo, ChangedFileObject{Path: f})
	}
	return cfo, nil
}

// UpdateCommitStatus for a given commit.
func (m *GiteaClient) UpdateCommitStatus(commitRef, baseContext, statusContext, status, targetURL, description string) error {
	if baseContext == "" {
		baseContext = "concourse-ci"
	}

	if statusContext == "" {
		statusContext = "status"
	}

	if targetURL == "" {
		targetURL = strings.Join([]string{os.Getenv("ATC_EXTERNAL_URL"), "builds", os.Getenv("BUILD_ID")}, "/")
	}

	if description == "" {
		description = fmt.Sprintf("Concourse CI build %s", status)
	}

	_, _, err := m.Client.CreateStatus(m.Owner, m.Repository, commitRef, gitea.CreateStatusOption{
		State:       gitea.StatusState(strings.ToLower(status)),
		TargetURL:   targetURL,
		Description: description,
		Context:     path.Join(baseContext, statusContext),
	})
	return err
}

// DeletePreviousComments removes prior comments posted by this resource.
func (m *GiteaClient) DeletePreviousComments(prNumber string, currentBuildURL string) error {
	pr, err := strconv.ParseInt(prNumber, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to convert pull request number to int: %w", err)
	}

	var comments []*gitea.Comment
	page := 1
	for {
		result, resp, err := m.Client.ListIssueComments(m.Owner, m.Repository, pr, gitea.ListIssueCommentOptions{
			ListOptions: gitea.ListOptions{Page: page, PageSize: 50},
		})
		if err != nil {
			return fmt.Errorf("failed to list gitea issue comments: %w", err)
		}
		if len(result) == 0 {
			break
		}
		comments = append(comments, result...)
		if resp != nil && resp.NextPage > 0 {
			page = resp.NextPage
		} else {
			page++
		}
	}

	for _, c := range comments {
		if !strings.Contains(c.Body, CommentMarkerPrefix) {
			continue
		}
		if currentBuildURL != "" {
			if buildURL := extractBuildURLFromMarker(c.Body); buildURL == currentBuildURL {
				continue
			}
		}
		if _, err := m.Client.DeleteIssueComment(m.Owner, m.Repository, c.ID); err != nil {
			return err
		}
	}

	return nil
}
