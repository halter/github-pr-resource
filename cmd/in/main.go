package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	resource "github.com/telia-oss/github-pr-resource"
)

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func main() {
	var request resource.GetRequest
	var decoder *json.Decoder
	var outputDir string
	var localDevelopmentBool = false
	localDevelopment, localDevelopmentPresent := os.LookupEnv("LOCAL_DEVELOPMENT")
	if localDevelopmentPresent {
		localDevelopmentBool, _ = strconv.ParseBool(localDevelopment)
	}
	if localDevelopmentBool {
		// local development (yippee!  Fire up that debugger)
		reader, _ := os.Open(os.Getenv("REQUEST_JSON"))
		decoder = json.NewDecoder(reader)
		outputDirPrefix := os.Getenv("OUTPUT_DIR_PREFIX")
		now := time.Now()
		outputDir = outputDirPrefix + "/" + fmt.Sprintf("%d", now.UnixMilli())
		if err := os.Mkdir(outputDir, 0777); err != nil {
			log.Fatalf("failed to create output directory: %s", err)
		}
	} else {
		// business as usual with original production code logic
		decoder = json.NewDecoder(os.Stdin)
		outputDir = os.Args[1]
	}
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		log.Fatalf("failed to unmarshal request: %s", err)
	}
	resource.PrintDebugInput(request.Source, request)
	if len(os.Args) < 2 && !localDevelopmentBool {
		log.Fatalf("missing arguments")
	}

	if err := request.Source.Validate(); err != nil {
		log.Fatalf("invalid source configuration: %s", err)
	}
	useGitea := request.Version.Provider == resource.ProviderGitea || !request.Source.HasGitHub()
	if useGitea {
		// Git operations authenticate against Gitea for Gitea pull requests.
		request.Source.AccessToken = request.Source.GiteaAccessToken
	}
	git, err := resource.NewGitClient(&request.Source, outputDir, os.Stderr)
	if err != nil {
		log.Fatalf("failed to create git client: %s", err)
	}
	var manager resource.Github
	if useGitea {
		manager, err = resource.NewGiteaClient(&request.Source)
		if err != nil {
			log.Fatalf("failed to create gitea manager: %s", err)
		}
	} else {
		manager, err = resource.NewGithubClient(&request.Source)
		if err != nil {
			log.Fatalf("failed to create github manager: %s", err)
		}
	}
	response, err := resource.Get(request, manager, git, outputDir)
	resource.SendToDataDog(request, err)
	if err != nil {
		if request.Params.PostStatusOnGetFailure {
			log.Printf("posting failure status for commit %s", request.Version.Commit)
			description := fmt.Sprintf("Get failed: %s", truncateString(err.Error(), 100))
			if statusErr := manager.UpdateCommitStatus(
				request.Version.Commit,
				request.Params.BaseContext,
				request.Params.Context,
				"failure",
				"",
				description,
			); statusErr != nil {
				log.Printf("failed to post failure status: %s", statusErr)
			}
		}
		log.Fatalf("get failed: %s", err)
	}
	if !useGitea {
		resource.PrintCurrentRateLimit(request.Source)
	}
	resource.PrintDebugOutput(request.Source, response)
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		log.Fatalf("failed to marshal response: %s", err)
	}
	resource.SayThanks()
}
