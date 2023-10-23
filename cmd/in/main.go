package main

import (
	"encoding/json"
	"log"
	"os"

	resource "github.com/telia-oss/github-pr-resource"
)

func main() {
	var request resource.GetRequest

	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		log.Fatalf("failed to unmarshal request: %s", err)
	}
	resource.PrintDebugInput(request.Source, request)
	if len(os.Args) < 2 {
		log.Fatalf("missing arguments")
	}
	outputDir := os.Args[1]
	if err := request.Source.Validate(); err != nil {
		log.Fatalf("invalid source configuration: %s", err)
	}
	git, err := resource.NewGitClient(&request.Source, outputDir, os.Stderr)
	if err != nil {
		log.Fatalf("failed to create git client: %s", err)
	}
	github, err := resource.NewGithubClient(&request.Source)
	if err != nil {
		log.Fatalf("failed to create github manager: %s", err)
	}
	response, err := resource.Get(request, github, git, outputDir)
	if err != nil {
		log.Fatalf("get failed: %s", err)
	}
	resource.PrintCurrentRateLimit(request.Source)
	resource.PrintDebugOutput(request.Source, response)
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		log.Fatalf("failed to marshal response: %s", err)
	}
	resource.SayThanks()
}
