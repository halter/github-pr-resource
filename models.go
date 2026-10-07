package resource

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/shurcooL/githubv4"
)

type StatusFilter struct {
	Context string
	State   string
}

// Source represents the configuration for the resource.
type Source struct {
	Repository              string                      `json:"repository"`
	AccessToken             string                      `json:"access_token"`
	OdAdvanced              OdAdvanced                  `json:"od_advanced"`
	V3Endpoint              string                      `json:"v3_endpoint"`
	V4Endpoint              string                      `json:"v4_endpoint"`
	Paths                   []string                    `json:"paths"`
	IgnorePaths             []string                    `json:"ignore_paths"`
	DisableCISkip           bool                        `json:"disable_ci_skip"`
	DisableGitLFS           bool                        `json:"disable_git_lfs"`
	SkipSSLVerification     bool                        `json:"skip_ssl_verification"`
	DisableForks            bool                        `json:"disable_forks"`
	IgnoreDrafts            bool                        `json:"ignore_drafts"`
	GitCryptKey             string                      `json:"git_crypt_key"`
	BaseBranch              string                      `json:"base_branch"`
	RequiredReviewApprovals int                         `json:"required_review_approvals"`
	Labels                  []string                    `json:"labels"`
	States                  []githubv4.PullRequestState `json:"states"`
	StatusFilters           []StatusFilter              `json:"status_filters"`
}

type OdAdvanced struct {
	VaultAddr                                             string `json:"vault_addr"`
	VaultApproleRoleId                                    string `json:"vault_approle_role_id"`
	VaultApproleSecretId                                  string `json:"vault_approle_secret_id"`
	MinRemainingThresholdBeforeUsingAccessTokenAdditional int    `json:"min_remaining_threshold_before_using_access_token_additional"`
	DataDogApiKey                                         string `json:"datadog_api_key"`
	DataDogAppKey                                         string `json:"datadog_app_key"`
	DataDogMetricName                                     string `json:"datadog_metric_name"`
	DataDogResourcesName                                  string `json:"datadog_resources_name"`
	DataDogResourcesType                                  string `json:"datadog_resources_type"`
	Debug                                                 bool   `json:"debug"`
}

// Validate the source configuration.
func (s *Source) Validate() error {
	if s.AccessToken == "" {
		return errors.New("access_token must be set")
	}
	if s.Repository == "" {
		return errors.New("repository must be set")
	}
	if s.V3Endpoint != "" && s.V4Endpoint == "" {
		return errors.New("v4_endpoint must be set together with v3_endpoint")
	}
	if s.V4Endpoint != "" && s.V3Endpoint == "" {
		return errors.New("v3_endpoint must be set together with v4_endpoint")
	}
	for _, state := range s.States {
		switch state {
		case githubv4.PullRequestStateOpen:
		case githubv4.PullRequestStateClosed:
		case githubv4.PullRequestStateMerged:
		default:
			return fmt.Errorf("states value \"%s\" must be one of: OPEN, MERGED, CLOSED", state)
		}
	}
	return nil
}

// Metadata output from get/put steps.
type Metadata []*MetadataField

// Add a MetadataField to the Metadata.
func (m *Metadata) Add(name, value string) {
	*m = append(*m, &MetadataField{Name: name, Value: value})
}

// MetadataField ...
type MetadataField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Version communicated with Concourse.
type Version struct {
	PR            string                    `json:"pr"`
	Commit        string                    `json:"commit"`
	CommittedDate time.Time                 `json:"committed,omitempty"`
	ChangedDate   time.Time                 `json:"changed,omitempty"`
	State         githubv4.PullRequestState `json:"state"`
}

// NewVersion constructs a new Version.
func NewVersion(p *PullRequest, changedDate time.Time) Version {
	return Version{
		PR:            strconv.Itoa(p.Number),
		Commit:        p.Tip.OID,
		State:         p.State,
		CommittedDate: p.Tip.CommittedDate.Time,
		ChangedDate:   changedDate,
	}
}

// IsPullRequestHint reports whether the version only names a pull request to re-check,
// as passed by `fly check-resource --from pr:N`, rather than a version check has produced.
func (v Version) IsPullRequestHint() bool {
	return v.PR != "" && v.ChangedDate.IsZero()
}

// PullRequest represents a pull request and includes the tip (commit).
type PullRequest struct {
	PullRequestObject
	Tip                 CommitObject
	ApprovedReviewCount int
	Labels              []LabelObject
}

// ChangedDate orders the version: when the tip was pushed to the pull request, or committed when
// the push is unknown, never earlier than the pull request was opened, closed or merged.
func (p *PullRequest) ChangedDate(pushed time.Time) time.Time {
	changed := p.Tip.CommittedDate.Time
	if !pushed.IsZero() {
		changed = pushed
	}
	if p.CreatedAt.After(changed) {
		changed = p.CreatedAt.Time
	}
	switch p.State {
	case githubv4.PullRequestStateClosed:
		if p.ClosedAt.After(changed) {
			changed = p.ClosedAt.Time
		}
	case githubv4.PullRequestStateMerged:
		if p.MergedAt.After(changed) {
			changed = p.MergedAt.Time
		}
	}
	return changed
}

// PullRequestObject represents the GraphQL commit node.
// https://developer.github.com/v4/object/pullrequest/
type PullRequestObject struct {
	ID          string
	Number      int
	Title       string
	URL         string
	BaseRefName string
	HeadRefName string
	Repository  struct {
		URL string
	}
	IsCrossRepository bool
	IsDraft           bool
	State             githubv4.PullRequestState
	CreatedAt         githubv4.DateTime
	UpdatedAt         githubv4.DateTime
	ClosedAt          githubv4.DateTime
	MergedAt          githubv4.DateTime
}

// CommitObject represents the GraphQL commit node.
// https://developer.github.com/v4/object/commit/
type CommitObject struct {
	ID            string
	OID           string
	CommittedDate githubv4.DateTime
	Message       string
	Author        struct {
		User struct {
			Login string
		}
		Email string
	}
	Status struct {
		Contexts []StatusContext
	}
}

type StatusContext struct {
	Context   string
	State     string
	CreatedAt githubv4.DateTime
}

// ChangedFileObject represents the GraphQL FilesChanged node.
// https://developer.github.com/v4/object/pullrequestchangedfile/
type ChangedFileObject struct {
	Path string
}

// LabelObject represents the GraphQL label node.
// https://developer.github.com/v4/object/label
type LabelObject struct {
	Name string
}
