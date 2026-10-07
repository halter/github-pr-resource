package resource

import (
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shurcooL/githubv4"
)

// Check (business logic)
func Check(request CheckRequest, manager Github) (CheckResponse, error) {
	var response CheckResponse

	// Filter out pull request if it does not have a filtered state
	filterStates := []githubv4.PullRequestState{githubv4.PullRequestStateOpen}
	if len(request.Source.States) > 0 {
		filterStates = request.Source.States
	}

	pulls, err := manager.ListPullRequests(filterStates)
	if err != nil {
		return nil, fmt.Errorf("failed to get last commits: %w", err)
	}

	// A version that only names a pull request (fly check-resource --from pr:N) asks for that
	// pull request's tip alone; it is not a version to order against or to return.
	if request.Version.IsPullRequestHint() {
		pulls = pullRequestNumbered(pulls, request.Version.PR)
		request.Version = Version{}
	}
	previousPR, _ := strconv.Atoi(request.Version.PR)

	disableSkipCI := request.Source.DisableCISkip

Loop:
	for _, p := range pulls {
		// [ci skip]/[skip ci] in Pull request title
		if !disableSkipCI && ContainsSkipCI(p.Title) {
			continue
		}

		// [ci skip]/[skip ci] in Commit message
		if !disableSkipCI && ContainsSkipCI(p.Tip.Message) {
			continue
		}

		// Filter pull request if the BaseBranch does not match the one specified in source
		if request.Source.BaseBranch != "" && p.BaseRefName != request.Source.BaseBranch {
			continue
		}

		versionTime, statusOK := statusVersionTime(p, request.Source.StatusFilters)
		if !statusOK {
			continue
		}

		// Skip pull requests with no activity since the last version, and the one that produced
		// it, before spending an API call on them.
		if !p.UpdatedAt.After(request.Version.ChangedDate) {
			continue
		}
		if p.Number == previousPR && p.Tip.OID == request.Version.Commit && p.State == request.Version.State && len(request.Source.StatusFilters) == 0 {
			continue
		}

		// Filter out pull request if it does not contain at least one of the desired labels
		if len(request.Source.Labels) > 0 {
			labelFound := false

		LabelLoop:
			for _, wantedLabel := range request.Source.Labels {
				for _, targetLabel := range p.Labels {
					if targetLabel.Name == wantedLabel {
						labelFound = true
						break LabelLoop
					}
				}
			}

			if !labelFound {
				continue Loop
			}
		}

		// Filter out forks.
		if request.Source.DisableForks && p.IsCrossRepository {
			continue
		}

		// Filter out drafts.
		if request.Source.IgnoreDrafts && p.IsDraft {
			continue
		}

		// Filter pull request if it does not have the required number of approved review(s).
		if p.ApprovedReviewCount < request.Source.RequiredReviewApprovals {
			continue
		}

		pushed, err := pushedDate(manager, p)
		if err != nil {
			return nil, err
		}
		changed := p.ChangedDate(pushed)

		// Filter out commits that are too old.
		if !changed.After(request.Version.ChangedDate) {
			continue
		}
		if len(request.Source.StatusFilters) == 0 {
			versionTime = changed
		}

		// Fetch files once if paths/ignore_paths are specified.
		var files []string

		if len(request.Source.Paths) > 0 || len(request.Source.IgnorePaths) > 0 {
			files, err = manager.ListModifiedFiles(p.Number)
			if err != nil {
				return nil, fmt.Errorf("failed to list modified files: %w", err)
			}
		}

		// Skip version if no files match the specified paths.
		if len(request.Source.Paths) > 0 {
			var wanted []string
			for _, pattern := range request.Source.Paths {
				w, err := FilterPath(files, pattern)
				if err != nil {
					return nil, fmt.Errorf("path match failed: %w", err)
				}
				wanted = append(wanted, w...)
			}
			if len(wanted) == 0 {
				continue Loop
			}
		}

		// Skip version if all files are ignored.
		if len(request.Source.IgnorePaths) > 0 {
			wanted := files
			for _, pattern := range request.Source.IgnorePaths {
				wanted, err = FilterIgnorePath(wanted, pattern)
				if err != nil {
					return nil, fmt.Errorf("ignore path match failed: %w", err)
				}
			}
			if len(wanted) == 0 {
				continue Loop
			}
		}
		response = append(response, NewVersion(p, versionTime))
	}

	// Sort the commits by date
	sort.Sort(response)

	// If there are no new but an old version = return the old
	if len(response) == 0 && request.Version.PR != "" {
		response = append(response, request.Version)
	}
	// If there are new versions and no previous = return just the latest
	if len(response) != 0 && request.Version.PR == "" {
		response = CheckResponse{response[len(response)-1]}
	}
	return response, nil
}

func pullRequestNumbered(pulls []*PullRequest, number string) []*PullRequest {
	for _, p := range pulls {
		if strconv.Itoa(p.Number) == number {
			return []*PullRequest{p}
		}
	}
	return nil
}

// statusVersionTime is the time of the newest status check the filters require, and whether the
// pull request's tip has every one of them.
func statusVersionTime(p *PullRequest, filters []StatusFilter) (time.Time, bool) {
	var versionTime time.Time
	for _, statusFilter := range filters {
		// "zero time - epoch = 0"
		versionTime = time.Time{}
		isValid := false
		for _, prStatus := range p.Tip.Status.Contexts {
			if prStatus.Context == statusFilter.Context && strings.EqualFold(prStatus.State, statusFilter.State) {
				// requires the given status exists and that it matches the desired state
				isValid = true
				// set the versionTime to the latest time of all
				// the status checks

				if prStatus.CreatedAt.After(versionTime) {
					versionTime = prStatus.CreatedAt.Time
				}
				break
			}
		}
		if !isValid {
			return time.Time{}, false
		}
	}
	return versionTime, true
}

// pushedDate is when the pull request's tip was pushed, or zero when the order does not depend on
// it: a fork's branch has no activity in this repository, a merged pull request is ordered by its
// merge, and a push GitHub has not recorded is ordered by its committer date.
func pushedDate(manager Github, p *PullRequest) (time.Time, error) {
	if p.IsCrossRepository || p.State == githubv4.PullRequestStateMerged {
		return time.Time{}, nil
	}
	pushed, found, err := manager.PushedDate(p.HeadRefName, p.Tip.OID)
	if err != nil {
		return time.Time{}, fmt.Errorf("pr %d: failed to look up when %s was pushed: %w", p.Number, p.Tip.OID, err)
	}
	if !found {
		log.Printf("pr %d: no push of %s recorded on %s, ordering it by its committer date", p.Number, p.Tip.OID, p.HeadRefName)
	}
	return pushed, nil
}

// ContainsSkipCI returns true if a string contains [ci skip] or [skip ci].
func ContainsSkipCI(s string) bool {
	re := regexp.MustCompile("(?i)\\[(ci skip|skip ci)\\]")
	return re.MatchString(s)
}

// FilterIgnorePath ...
func FilterIgnorePath(files []string, pattern string) ([]string, error) {
	var out []string
	for _, file := range files {
		match, err := filepath.Match(pattern, file)
		if err != nil {
			return nil, err
		}
		if !match && !IsInsidePath(pattern, file) {
			out = append(out, file)
		}
	}
	return out, nil
}

// FilterPath ...
func FilterPath(files []string, pattern string) ([]string, error) {
	var out []string
	for _, file := range files {
		match, err := filepath.Match(pattern, file)
		if err != nil {
			return nil, err
		}
		if match || IsInsidePath(pattern, file) {
			out = append(out, file)
		}
	}
	return out, nil
}

// IsInsidePath checks whether the child path is inside the parent path.
//
// /foo/bar is inside /foo, but /foobar is not inside /foo.
// /foo is inside /foo, but /foo is not inside /foo/
func IsInsidePath(parent, child string) bool {
	if parent == child {
		return true
	}

	// we add a trailing slash so that we only get prefix matches on a
	// directory separator
	parentWithTrailingSlash := parent
	if !strings.HasSuffix(parentWithTrailingSlash, string(filepath.Separator)) {
		parentWithTrailingSlash += string(filepath.Separator)
	}

	return strings.HasPrefix(child, parentWithTrailingSlash)
}

// CheckRequest ...
type CheckRequest struct {
	Source  Source  `json:"source"`
	Version Version `json:"version"`
}

// CheckResponse ...
type CheckResponse []Version

func (r CheckResponse) Len() int {
	return len(r)
}

func (r CheckResponse) Less(i, j int) bool {
	return r[j].ChangedDate.After(r[i].ChangedDate)
}

func (r CheckResponse) Swap(i, j int) {
	r[i], r[j] = r[j], r[i]
}
