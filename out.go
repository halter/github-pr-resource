package resource

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const CommentMarkerPrefix = "concourse-ci/github-pr-resource"

func currentBuildURL() string {
	atcURL := os.Getenv("ATC_EXTERNAL_URL")
	if atcURL == "" {
		return ""
	}
	return atcURL + "/builds/" + os.Getenv("BUILD_ID")
}

func buildCommentMarker() string {
	return fmt.Sprintf("\n<!-- %s build:%s -->", CommentMarkerPrefix, currentBuildURL())
}

// Put (business logic)
func Put(request PutRequest, manager Github, inputDir string) (*PutResponse, error) {
	if err := request.Params.Validate(); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	path := filepath.Join(inputDir, request.Params.Path, ".git", "resource")

	// Version available after a GET step.
	var version Version
	content, err := ioutil.ReadFile(filepath.Join(path, "version.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to read version from path: %w", err)
	}
	if err := json.Unmarshal(content, &version); err != nil {
		return nil, fmt.Errorf("failed to unmarshal version from file: %w", err)
	}

	// Metadata available after a GET step (optional - may not exist if get used skip_download).
	var metadata Metadata
	content, err = ioutil.ReadFile(filepath.Join(path, "metadata.json"))
	if err != nil {
		log.Printf("warning: failed to read metadata from path (get step may have used skip_download): %s", err)
	} else if err := json.Unmarshal(content, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata from file: %w", err)
	}

	// Set status if specified
	if p := request.Params; p.Status != "" {
		description := p.Description

		// Set description from a file
		if p.DescriptionFile != "" {
			content, err := ioutil.ReadFile(filepath.Join(inputDir, p.DescriptionFile))
			if err != nil {
				return nil, fmt.Errorf("failed to read description file: %w", err)
			}
			description = string(content)
		}

		if err := manager.UpdateCommitStatus(version.Commit, p.BaseContext, safeExpandEnv(p.Context), p.Status, safeExpandEnv(p.TargetURL), description); err != nil {
			return nil, fmt.Errorf("failed to set status: %w", err)
		} else {
			log.Printf("status : %s\n", p.Status)
		}
	}

	// Delete previous comments if specified
	if request.Params.DeletePreviousComments {
		if err := manager.DeletePreviousComments(version.PR, currentBuildURL()); err != nil {
			return nil, fmt.Errorf("failed to delete previous comments: %w", err)
		}
	}

	// Set comment if specified
	if p := request.Params; p.Comment != "" {
		if err := manager.PostComment(version.PR, safeExpandEnv(p.Comment)+buildCommentMarker()); err != nil {
			return nil, fmt.Errorf("failed to post comment: %w", err)
		}
	}

	// Set comment from a file
	if p := request.Params; p.CommentFile != "" {
		content, err := ioutil.ReadFile(filepath.Join(inputDir, p.CommentFile))
		if err != nil {
			return nil, fmt.Errorf("failed to read comment file: %w", err)
		}
		comment := string(content)
		if comment != "" {
			if err := manager.PostComment(version.PR, safeExpandEnv(comment)+buildCommentMarker()); err != nil {
				return nil, fmt.Errorf("failed to post comment: %w", err)
			}
		}
	}

	return &PutResponse{
		Version:  version,
		Metadata: metadata,
	}, nil
}

// PutRequest ...
type PutRequest struct {
	Source Source        `json:"source"`
	Params PutParameters `json:"params"`
}

// PutResponse ...
type PutResponse struct {
	Version  Version  `json:"version"`
	Metadata Metadata `json:"metadata,omitempty"`
}

// PutParameters for the resource.
type PutParameters struct {
	Path                   string `json:"path"`
	BaseContext            string `json:"base_context"`
	Context                string `json:"context"`
	TargetURL              string `json:"target_url"`
	DescriptionFile        string `json:"description_file"`
	Description            string `json:"description"`
	Status                 string `json:"status"`
	CommentFile            string `json:"comment_file"`
	Comment                string `json:"comment"`
	DeletePreviousComments bool   `json:"delete_previous_comments"`
}

// Validate the put parameters.
func (p *PutParameters) Validate() error {
	if p.Status == "" {
		return nil
	}
	// Make sure we are setting an allowed status
	var allowedStatus bool

	status := strings.ToLower(p.Status)
	allowed := []string{"success", "pending", "failure", "error"}

	for _, a := range allowed {
		if status == a {
			allowedStatus = true
		}
	}

	if !allowedStatus {
		return fmt.Errorf("unknown status: %s", p.Status)
	}

	return nil
}

func safeExpandEnv(s string) string {
	return os.Expand(s, func(v string) string {
		switch v {
		case "BUILD_ID", "BUILD_NAME", "BUILD_JOB_NAME", "BUILD_PIPELINE_NAME", "BUILD_TEAM_NAME", "ATC_EXTERNAL_URL":
			return os.Getenv(v)
		}
		return "$" + v
	})
}
