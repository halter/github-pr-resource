package resource_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-github/v81/github"
	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/assert"
	resource "github.com/telia-oss/github-pr-resource"
	"github.com/telia-oss/github-pr-resource/fakes"
)

var (
	checkBase = time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)

	testPullRequests = []*resource.PullRequest{
		createTestPR(1, "master", true, false, 0, nil, false, githubv4.PullRequestStateOpen, []resource.StatusContext{}),
		createTestPR(2, "master", false, false, 0, nil, false, githubv4.PullRequestStateOpen, []resource.StatusContext{}),
		createTestPR(3, "master", false, false, 0, nil, true, githubv4.PullRequestStateOpen, []resource.StatusContext{}),
		createTestPR(4, "master", false, false, 0, nil, false, githubv4.PullRequestStateOpen, []resource.StatusContext{}),
		createTestPR(5, "master", false, true, 0, nil, false, githubv4.PullRequestStateOpen, []resource.StatusContext{}),
		createTestPR(6, "master", false, false, 0, nil, false, githubv4.PullRequestStateOpen, []resource.StatusContext{}),
		createTestPR(7, "develop", false, false, 0, []string{"enhancement"}, false, githubv4.PullRequestStateOpen, []resource.StatusContext{}),
		createTestPR(8, "master", false, false, 1, []string{"wontfix"}, false, githubv4.PullRequestStateOpen, []resource.StatusContext{}),
		createTestPR(9, "master", false, false, 0, nil, false, githubv4.PullRequestStateOpen, []resource.StatusContext{}),
		createTestPR(10, "master", false, false, 0, nil, false, githubv4.PullRequestStateClosed, []resource.StatusContext{}),
		createTestPR(11, "master", false, false, 0, nil, false, githubv4.PullRequestStateMerged, []resource.StatusContext{}),
		createTestPR(12, "master", false, false, 0, nil, false, githubv4.PullRequestStateOpen, []resource.StatusContext{}),

		createTestPR(13, "master", false, false, 0, nil, false, githubv4.PullRequestStateOpen, []resource.StatusContext{
			{Context: "my-status-check", State: "SUCCESS"},
		}),
		// multiple status check
		createTestPR(14, "master", false, false, 0, nil, false, githubv4.PullRequestStateOpen, []resource.StatusContext{
			{Context: "my-status-check", State: "SUCCESS"},
			{Context: "my-failed-status-check", State: "FAILURE"},
		}),
		createTestPR(15, "master", false, false, 0, nil, false, githubv4.PullRequestStateOpen, []resource.StatusContext{
			{Context: "my-status-check-2", State: "SUCCESS", CreatedAt: githubv4.DateTime{Time: time.Now().AddDate(0, 0, 1)}},
		}),
	}
)

// testVersion is the version check produces for a test pull request pushed when it was committed.
func testVersion(p *resource.PullRequest) resource.Version {
	return resource.NewVersion(p, p.Tip.CommittedDate.Time)
}

func datedPR(number int, committed, updated time.Time) *resource.PullRequest {
	p := createTestPR(number, "master", false, false, 0, nil, false, githubv4.PullRequestStateOpen, nil)
	p.Tip.CommittedDate = githubv4.DateTime{Time: committed}
	p.UpdatedAt = githubv4.DateTime{Time: updated}
	p.ClosedAt = githubv4.DateTime{Time: updated}
	p.MergedAt = githubv4.DateTime{Time: updated}
	return p
}

type pushLookup struct {
	at      time.Time
	missing bool
	err     error
}

func TestCheck(t *testing.T) {
	rebased := datedPR(21, checkBase.Add(-5*24*time.Hour), checkBase.Add(-time.Hour))
	previous := datedPR(22, checkBase.Add(-2*24*time.Hour), checkBase.Add(-2*24*time.Hour))
	previousVersion := testVersion(previous)

	forked := datedPR(23, checkBase.Add(-time.Hour), checkBase.Add(-time.Hour))
	forked.IsCrossRepository = true

	reopened := datedPR(24, checkBase.Add(-5*24*time.Hour), checkBase.Add(-time.Hour))
	reopened.CreatedAt = githubv4.DateTime{Time: checkBase.Add(-time.Hour)}

	merged := datedPR(25, checkBase.Add(-5*24*time.Hour), checkBase.Add(-time.Hour))
	merged.State = githubv4.PullRequestStateMerged

	draft := datedPR(26, checkBase.Add(-time.Hour), checkBase.Add(-time.Hour))
	draft.IsDraft = true

	unrecorded := datedPR(27, checkBase.Add(-time.Hour), checkBase.Add(-time.Hour))
	commented := datedPR(previous.Number, previous.Tip.CommittedDate.Time, checkBase.Add(-time.Hour))

	tests := []struct {
		description  string
		source       resource.Source
		version      resource.Version
		files        [][]string
		pullRequests []*resource.PullRequest
		pushed       map[string]pushLookup
		pushLookups  *int
		expected     resource.CheckResponse
		wantErr      bool
	}{
		{
			description:  "check orders a rebased commit by when it was pushed rather than committed",
			source:       resource.Source{Repository: "itsdalmo/test-repository", AccessToken: "oauthtoken"},
			version:      previousVersion,
			pullRequests: []*resource.PullRequest{rebased, previous},
			pushed:       map[string]pushLookup{rebased.Tip.OID: {at: checkBase.Add(-time.Hour)}},
			expected:     resource.CheckResponse{resource.NewVersion(rebased, checkBase.Add(-time.Hour))},
		},
		{
			description:  "check skips pull requests with no activity since the last version without looking up pushes",
			source:       resource.Source{Repository: "itsdalmo/test-repository", AccessToken: "oauthtoken"},
			version:      previousVersion,
			pullRequests: []*resource.PullRequest{datedPR(21, checkBase.Add(-5*24*time.Hour), checkBase.Add(-3*24*time.Hour)), previous},
			pushLookups:  github.Ptr(0),
			expected:     resource.CheckResponse{previousVersion},
		},
		{
			description:  "check does not produce a version for a pull request that was only commented on",
			source:       resource.Source{Repository: "itsdalmo/test-repository", AccessToken: "oauthtoken"},
			version:      previousVersion,
			pullRequests: []*resource.PullRequest{rebased, previous},
			pushed:       map[string]pushLookup{rebased.Tip.OID: {at: checkBase.Add(-5 * 24 * time.Hour)}},
			expected:     resource.CheckResponse{previousVersion},
		},
		{
			description:  "check does not look up the pull request that produced the previous version",
			source:       resource.Source{Repository: "itsdalmo/test-repository", AccessToken: "oauthtoken"},
			version:      previousVersion,
			pullRequests: []*resource.PullRequest{commented},
			pushLookups:  github.Ptr(0),
			expected:     resource.CheckResponse{previousVersion},
		},
		{
			description:  "check orders a pull request whose push is not recorded by its committer date",
			source:       resource.Source{Repository: "itsdalmo/test-repository", AccessToken: "oauthtoken"},
			version:      previousVersion,
			pullRequests: []*resource.PullRequest{unrecorded, previous},
			pushed:       map[string]pushLookup{unrecorded.Tip.OID: {missing: true}},
			expected:     resource.CheckResponse{testVersion(unrecorded)},
		},
		{
			description:  "check fails rather than skipping a pull request when the push lookup fails",
			source:       resource.Source{Repository: "itsdalmo/test-repository", AccessToken: "oauthtoken"},
			version:      previousVersion,
			pullRequests: []*resource.PullRequest{rebased, previous},
			pushed:       map[string]pushLookup{rebased.Tip.OID: {err: errors.New("rate limited")}},
			wantErr:      true,
		},
		{
			description:  "check orders a fork's pull request by committer date without looking up pushes",
			source:       resource.Source{Repository: "itsdalmo/test-repository", AccessToken: "oauthtoken"},
			version:      previousVersion,
			pullRequests: []*resource.PullRequest{forked, previous},
			pushLookups:  github.Ptr(0),
			expected:     resource.CheckResponse{resource.NewVersion(forked, checkBase.Add(-time.Hour))},
		},
		{
			description:  "check orders a pull request opened after its branch was pushed by its creation date",
			source:       resource.Source{Repository: "itsdalmo/test-repository", AccessToken: "oauthtoken"},
			version:      previousVersion,
			pullRequests: []*resource.PullRequest{reopened, previous},
			pushed:       map[string]pushLookup{reopened.Tip.OID: {at: checkBase.Add(-5 * 24 * time.Hour)}},
			expected:     resource.CheckResponse{resource.NewVersion(reopened, checkBase.Add(-time.Hour))},
		},
		{
			description:  "check orders a merged pull request by when it was merged",
			source:       resource.Source{Repository: "itsdalmo/test-repository", AccessToken: "oauthtoken", States: []githubv4.PullRequestState{githubv4.PullRequestStateMerged}},
			version:      previousVersion,
			pullRequests: []*resource.PullRequest{merged},
			pushLookups:  github.Ptr(0),
			expected:     resource.CheckResponse{resource.NewVersion(merged, checkBase.Add(-time.Hour))},
		},
		{
			description:  "check with a pull request hint produces only that pull request's tip whatever its dates",
			source:       resource.Source{Repository: "itsdalmo/test-repository", AccessToken: "oauthtoken"},
			version:      resource.Version{PR: "21"},
			pullRequests: []*resource.PullRequest{rebased, previous, forked},
			pushed:       map[string]pushLookup{rebased.Tip.OID: {at: checkBase.Add(-5 * 24 * time.Hour)}},
			pushLookups:  github.Ptr(1),
			expected:     resource.CheckResponse{resource.NewVersion(rebased, checkBase.Add(-5*24*time.Hour))},
		},
		{
			description:  "check with a pull request hint falls back to the committer date when the push is unknown",
			source:       resource.Source{Repository: "itsdalmo/test-repository", AccessToken: "oauthtoken"},
			version:      resource.Version{PR: "21"},
			pullRequests: []*resource.PullRequest{rebased, previous},
			pushed:       map[string]pushLookup{rebased.Tip.OID: {missing: true}},
			expected:     resource.CheckResponse{testVersion(rebased)},
		},
		{
			description:  "check with a pull request hint for a filtered pull request produces nothing",
			source:       resource.Source{Repository: "itsdalmo/test-repository", AccessToken: "oauthtoken", IgnoreDrafts: true},
			version:      resource.Version{PR: "26"},
			pullRequests: []*resource.PullRequest{draft, previous},
			expected:     resource.CheckResponse(nil),
		},
		{
			description: "check returns the latest version if there is no previous",
			source: resource.Source{
				Repository:    "itsdalmo/test-repository",
				AccessToken:   "oauthtoken",
				StatusFilters: []resource.StatusFilter{},
			},
			version:      resource.Version{},
			pullRequests: testPullRequests,
			files:        [][]string{},
			expected: resource.CheckResponse{
				testVersion(testPullRequests[1]),
			},
		},

		{
			description: "check returns the previous version when its still latest",
			source: resource.Source{
				Repository:  "itsdalmo/test-repository",
				AccessToken: "oauthtoken",
			},
			version:      testVersion(testPullRequests[1]),
			pullRequests: testPullRequests,
			files:        [][]string{},
			expected: resource.CheckResponse{
				testVersion(testPullRequests[1]),
			},
		},

		{
			description: "check returns all new versions since the last",
			source: resource.Source{
				Repository:  "itsdalmo/test-repository",
				AccessToken: "oauthtoken",
			},
			version:      testVersion(testPullRequests[3]),
			pullRequests: testPullRequests,
			files:        [][]string{},
			expected: resource.CheckResponse{
				testVersion(testPullRequests[2]),
				testVersion(testPullRequests[1]),
			},
		},

		{
			description: "check will only return versions that match the specified paths",
			source: resource.Source{
				Repository:  "itsdalmo/test-repository",
				AccessToken: "oauthtoken",
				Paths:       []string{"terraform/*/*.tf", "terraform/*/*/*.tf"},
			},
			version:      testVersion(testPullRequests[3]),
			pullRequests: testPullRequests,
			files: [][]string{
				{"README.md", "travis.yml"},
				{"terraform/modules/ecs/main.tf", "README.md"},
				{"terraform/modules/variables.tf", "travis.yml"},
			},
			expected: resource.CheckResponse{
				testVersion(testPullRequests[2]),
			},
		},

		{
			description: "check will skip versions which only match the ignore paths",
			source: resource.Source{
				Repository:  "itsdalmo/test-repository",
				AccessToken: "oauthtoken",
				IgnorePaths: []string{"*.md", "*.yml"},
			},
			version:      testVersion(testPullRequests[3]),
			pullRequests: testPullRequests,
			files: [][]string{
				{"README.md", "travis.yml"},
				{"terraform/modules/ecs/main.tf", "README.md"},
				{"terraform/modules/variables.tf", "travis.yml"},
			},
			expected: resource.CheckResponse{
				testVersion(testPullRequests[2]),
			},
		},

		{
			description: "check correctly ignores [skip ci] when specified",
			source: resource.Source{
				Repository:    "itsdalmo/test-repository",
				AccessToken:   "oauthtoken",
				DisableCISkip: true,
			},
			version:      testVersion(testPullRequests[1]),
			pullRequests: testPullRequests,
			expected: resource.CheckResponse{
				testVersion(testPullRequests[0]),
			},
		},

		{
			description: "check correctly ignores drafts when drafts are ignored",
			source: resource.Source{
				Repository:   "itsdalmo/test-repository",
				AccessToken:  "oauthtoken",
				IgnoreDrafts: true,
			},
			version:      testVersion(testPullRequests[3]),
			pullRequests: testPullRequests,
			expected: resource.CheckResponse{
				testVersion(testPullRequests[1]),
			},
		},

		{
			description: "check does not ignore drafts when drafts are not ignored",
			source: resource.Source{
				Repository:   "itsdalmo/test-repository",
				AccessToken:  "oauthtoken",
				IgnoreDrafts: false,
			},
			version:      testVersion(testPullRequests[3]),
			pullRequests: testPullRequests,
			expected: resource.CheckResponse{
				testVersion(testPullRequests[2]),
				testVersion(testPullRequests[1]),
			},
		},

		{
			description: "check correctly ignores cross repo pull requests",
			source: resource.Source{
				Repository:   "itsdalmo/test-repository",
				AccessToken:  "oauthtoken",
				DisableForks: true,
			},
			version:      testVersion(testPullRequests[5]),
			pullRequests: testPullRequests,
			expected: resource.CheckResponse{
				testVersion(testPullRequests[3]),
				testVersion(testPullRequests[2]),
				testVersion(testPullRequests[1]),
			},
		},

		{
			description: "check supports specifying base branch",
			source: resource.Source{
				Repository:  "itsdalmo/test-repository",
				AccessToken: "oauthtoken",
				BaseBranch:  "develop",
			},
			version:      resource.Version{},
			pullRequests: testPullRequests,
			files:        [][]string{},
			expected: resource.CheckResponse{
				testVersion(testPullRequests[6]),
			},
		},

		{
			description: "check correctly ignores PRs with no approved reviews when specified",
			source: resource.Source{
				Repository:              "itsdalmo/test-repository",
				AccessToken:             "oauthtoken",
				RequiredReviewApprovals: 1,
			},
			version:      testVersion(testPullRequests[8]),
			pullRequests: testPullRequests,
			expected: resource.CheckResponse{
				testVersion(testPullRequests[7]),
			},
		},

		{
			description: "check returns latest version from a PR with at least one of the desired labels on it",
			source: resource.Source{
				Repository:  "itsdalmo/test-repository",
				AccessToken: "oauthtoken",
				Labels:      []string{"enhancement"},
			},
			version:      resource.Version{},
			pullRequests: testPullRequests,
			files:        [][]string{},
			expected: resource.CheckResponse{
				testVersion(testPullRequests[6]),
			},
		},

		{
			description: "check returns latest version from a PR with a single state filter",
			source: resource.Source{
				Repository:  "itsdalmo/test-repository",
				AccessToken: "oauthtoken",
				States:      []githubv4.PullRequestState{githubv4.PullRequestStateClosed},
			},
			version:      resource.Version{},
			pullRequests: testPullRequests,
			files:        [][]string{},
			expected: resource.CheckResponse{
				testVersion(testPullRequests[9]),
			},
		},

		{
			description: "check filters out versions from a PR which do not match the state filter",
			source: resource.Source{
				Repository:  "itsdalmo/test-repository",
				AccessToken: "oauthtoken",
				States:      []githubv4.PullRequestState{githubv4.PullRequestStateOpen},
			},
			version:      resource.Version{},
			pullRequests: testPullRequests[9:11],
			files:        [][]string{},
			expected:     resource.CheckResponse(nil),
		},
		{
			description: "check returns versions from a PR with multiple state filters",
			source: resource.Source{
				Repository:  "itsdalmo/test-repository",
				AccessToken: "oauthtoken",
				States:      []githubv4.PullRequestState{githubv4.PullRequestStateClosed, githubv4.PullRequestStateMerged},
			},
			version:      testVersion(testPullRequests[11]),
			pullRequests: testPullRequests,
			files:        [][]string{},
			expected: resource.CheckResponse{
				testVersion(testPullRequests[10]),
				testVersion(testPullRequests[9]),
			},
		},
		{
			description: "check returns a PR that has a complete status for a status check",
			source: resource.Source{
				Repository:  "itsdalmo/test-repository",
				AccessToken: "oauthtoken",
				StatusFilters: []resource.StatusFilter{
					{Context: "my-status-check", State: "success"},
				},
			},
			version:      resource.Version{},
			pullRequests: testPullRequests,
			files:        [][]string{},
			expected: resource.CheckResponse{
				testVersion(testPullRequests[12]),
			},
		},
		{
			description: "check returns a PR where the check created_at is greater than the resource version",
			source: resource.Source{
				Repository:  "itsdalmo/test-repository",
				AccessToken: "oauthtoken",
				StatusFilters: []resource.StatusFilter{
					{Context: "my-status-check-2", State: "success"},
				},
			},
			version:      testVersion(testPullRequests[9]),
			pullRequests: testPullRequests,
			files:        [][]string{},
			expected: resource.CheckResponse{
				testVersion(testPullRequests[9]),
			},
		},
		{
			description: "check returns a PR that has multiple required status checks",
			source: resource.Source{
				Repository:  "itsdalmo/test-repository",
				AccessToken: "oauthtoken",
				StatusFilters: []resource.StatusFilter{
					{Context: "my-status-check", State: "success"},
					{Context: "my-failed-status-check", State: "failure"},
				},
			},
			version:      resource.Version{},
			pullRequests: testPullRequests,
			files:        [][]string{},
			expected: resource.CheckResponse{
				// todo: pull request index
				testVersion(testPullRequests[13]),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			fake := new(fakes.FakeGithub)
			pullRequests := []*resource.PullRequest{}
			filterStates := []githubv4.PullRequestState{githubv4.PullRequestStateOpen}
			if len(tc.source.States) > 0 {
				filterStates = tc.source.States
			}
			for i := range tc.pullRequests {
				for j := range filterStates {
					if filterStates[j] == tc.pullRequests[i].State {
						pullRequests = append(pullRequests, tc.pullRequests[i])
						break
					}
				}
			}
			fake.ListPullRequestsReturns(pullRequests, nil)
			fake.PushedDateStub = func(branch, commit string) (time.Time, bool, error) {
				if lookup, ok := tc.pushed[commit]; ok {
					return lookup.at, !lookup.missing, lookup.err
				}
				for _, p := range tc.pullRequests {
					if p.Tip.OID == commit {
						assert.Equal(t, p.HeadRefName, branch)
						return p.Tip.CommittedDate.Time, true, nil
					}
				}
				return time.Time{}, false, nil
			}

			for i, file := range tc.files {
				fake.ListModifiedFilesReturnsOnCall(i, file, nil)
			}

			input := resource.CheckRequest{Source: tc.source, Version: tc.version}
			output, err := resource.Check(input, fake)

			if tc.wantErr {
				assert.Error(t, err)
				assert.Nil(t, output)
			} else if assert.NoError(t, err) {
				assert.Equal(t, tc.expected, output)
			}
			assert.Equal(t, 1, fake.ListPullRequestsCallCount())
			if tc.pushLookups != nil {
				assert.Equal(t, *tc.pushLookups, fake.PushedDateCallCount())
			}
		})
	}
}

func TestContainsSkipCI(t *testing.T) {
	tests := []struct {
		description string
		message     string
		want        bool
	}{
		{
			description: "does not just match any symbol in the regexp",
			message:     "(",
			want:        false,
		},
		{
			description: "does not match when it should not",
			message:     "test",
			want:        false,
		},
		{
			description: "matches [ci skip]",
			message:     "[ci skip]",
			want:        true,
		},
		{
			description: "matches [skip ci]",
			message:     "[skip ci]",
			want:        true,
		},
		{
			description: "matches trailing skip ci",
			message:     "trailing [skip ci]",
			want:        true,
		},
		{
			description: "matches leading skip ci",
			message:     "[skip ci] leading",
			want:        true,
		},
		{
			description: "is case insensitive",
			message:     "case[Skip CI]insensitive",
			want:        true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := resource.ContainsSkipCI(tc.message)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFilterPath(t *testing.T) {
	cases := []struct {
		description string
		pattern     string
		files       []string
		want        []string
	}{
		{
			description: "returns all matching files",
			pattern:     "*.txt",
			files: []string{
				"file1.txt",
				"test/file2.txt",
			},
			want: []string{
				"file1.txt",
			},
		},
		{
			description: "works with wildcard",
			pattern:     "test/*",
			files: []string{
				"file1.txt",
				"test/file2.txt",
			},
			want: []string{
				"test/file2.txt",
			},
		},
		{
			description: "excludes unmatched files",
			pattern:     "*/*.txt",
			files: []string{
				"test/file1.go",
				"test/file2.txt",
			},
			want: []string{
				"test/file2.txt",
			},
		},
		{
			description: "handles prefix matches",
			pattern:     "foo/",
			files: []string{
				"foo/a",
				"foo/a.txt",
				"foo/a/b/c/d.txt",
				"foo",
				"bar",
				"bar/a.txt",
			},
			want: []string{
				"foo/a",
				"foo/a.txt",
				"foo/a/b/c/d.txt",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			got, err := resource.FilterPath(tc.files, tc.pattern)
			if assert.NoError(t, err) {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestFilterIgnorePath(t *testing.T) {
	cases := []struct {
		description string
		pattern     string
		files       []string
		want        []string
	}{
		{
			description: "excludes all matching files",
			pattern:     "*.txt",
			files: []string{
				"file1.txt",
				"test/file2.txt",
			},
			want: []string{
				"test/file2.txt",
			},
		},
		{
			description: "works with wildcard",
			pattern:     "test/*",
			files: []string{
				"file1.txt",
				"test/file2.txt",
			},
			want: []string{
				"file1.txt",
			},
		},
		{
			description: "includes unmatched files",
			pattern:     "*/*.txt",
			files: []string{
				"test/file1.go",
				"test/file2.txt",
			},
			want: []string{
				"test/file1.go",
			},
		},
		{
			description: "handles prefix matches",
			pattern:     "foo/",
			files: []string{
				"foo/a",
				"foo/a.txt",
				"foo/a/b/c/d.txt",
				"foo",
				"bar",
				"bar/a.txt",
			},
			want: []string{
				"foo",
				"bar",
				"bar/a.txt",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			got, err := resource.FilterIgnorePath(tc.files, tc.pattern)
			if assert.NoError(t, err) {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestIsInsidePath(t *testing.T) {
	cases := []struct {
		description string
		parent      string

		expectChildren    []string
		expectNotChildren []string

		want bool
	}{
		{
			description: "basic test",
			parent:      "foo/bar",
			expectChildren: []string{
				"foo/bar",
				"foo/bar/baz",
			},
			expectNotChildren: []string{
				"foo/barbar",
				"foo/baz/bar",
			},
		},
		{
			description: "does not match parent directories against child files",
			parent:      "foo/",
			expectChildren: []string{
				"foo/bar",
			},
			expectNotChildren: []string{
				"foo",
			},
		},
		{
			description: "matches parents without trailing slash",
			parent:      "foo/bar",
			expectChildren: []string{
				"foo/bar",
				"foo/bar/baz",
			},
		},
		{
			description: "handles children that are shorter than the parent",
			parent:      "foo/bar/baz",
			expectNotChildren: []string{
				"foo",
				"foo/bar",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			for _, expectedChild := range tc.expectChildren {
				if !resource.IsInsidePath(tc.parent, expectedChild) {
					t.Errorf("Expected \"%s\" to be inside \"%s\"", expectedChild, tc.parent)
				}
			}

			for _, expectedNotChild := range tc.expectNotChildren {
				if resource.IsInsidePath(tc.parent, expectedNotChild) {
					t.Errorf("Expected \"%s\" to not be inside \"%s\"", expectedNotChild, tc.parent)
				}
			}
		})
	}
}
