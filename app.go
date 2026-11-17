package resource

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v81/github"
)

const (
	appJWTLifetime     = 9 * time.Minute
	appJWTClockSkew    = 60 * time.Second
	appRequestDeadline = 30 * time.Second
)

// UsesGitHubApp reports whether the source authenticates as a GitHub App instead of with access_token.
func (s *Source) UsesGitHubApp() bool {
	return s.AppID != "" || s.PrivateKey != ""
}

// ResolveAccessToken mints an installation access token for a source that authenticates as a GitHub App
// and stores it as the source's access token. Sources that set access_token are left as they are.
func (s *Source) ResolveAccessToken() error {
	if !s.UsesGitHubApp() {
		return nil
	}
	owner, repository, err := parseRepository(s.Repository)
	if err != nil {
		return err
	}
	client, err := s.appClient(time.Now())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), appRequestDeadline)
	defer cancel()

	var installationID int64
	if s.InstallationID != "" {
		installationID, err = s.InstallationID.Int64()
		if err != nil {
			return fmt.Errorf("installation_id must be an integer: %w", err)
		}
	} else {
		installation, _, err := client.Apps.FindRepositoryInstallation(ctx, owner, repository)
		if err != nil {
			return fmt.Errorf("finding the GitHub App installation on %s: %w", s.Repository, err)
		}
		installationID = installation.GetID()
	}

	minted, _, err := client.Apps.CreateInstallationToken(ctx, installationID, nil)
	if err != nil {
		return fmt.Errorf("minting an installation access token for %s: %w", s.Repository, err)
	}
	if minted.GetToken() == "" {
		return fmt.Errorf("installation access token response for %s carries no token", s.Repository)
	}
	s.AccessToken = minted.GetToken()
	log.Printf("authenticated as GitHub App %s, installation %d\n", s.AppID, installationID)
	return nil
}

func (s *Source) appClient(now time.Time) (*github.Client, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(s.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("parsing private_key: %w", err)
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    s.AppID.String(),
		IssuedAt:  jwt.NewNumericDate(now.Add(-appJWTClockSkew)),
		ExpiresAt: jwt.NewNumericDate(now.Add(appJWTLifetime)),
	}).SignedString(key)
	if err != nil {
		return nil, fmt.Errorf("signing the GitHub App JWT: %w", err)
	}

	httpClient := &http.Client{}
	if s.SkipSSLVerification {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	client := github.NewClient(httpClient).WithAuthToken(signed)
	if s.V3Endpoint != "" {
		return client.WithEnterpriseURLs(s.V3Endpoint, s.V3Endpoint)
	}
	return client, nil
}
