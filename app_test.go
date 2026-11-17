package resource_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	resource "github.com/telia-oss/github-pr-resource"
)

func generateAppKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	encoded := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return key, string(encoded)
}

type appServer struct {
	*httptest.Server
	installationLookups int
	tokenRequests       []string
}

func newAppServer(t *testing.T, key *rsa.PrivateKey, appID string) *appServer {
	t.Helper()
	server := &appServer{}
	authenticate := func(r *http.Request) {
		signed := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		claims := jwt.RegisteredClaims{}
		_, err := jwt.ParseWithClaims(signed, &claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil },
			jwt.WithValidMethods([]string{"RS256"}))
		require.NoError(t, err)
		assert.Equal(t, appID, claims.Issuer)
		assert.LessOrEqual(t, time.Until(claims.ExpiresAt.Time), 10*time.Minute)
		assert.True(t, claims.IssuedAt.Time.Before(time.Now()))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/repos/halter/repo/installation", func(w http.ResponseWriter, r *http.Request) {
		authenticate(r)
		server.installationLookups++
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 42})
	})
	mux.HandleFunc("POST /api/v3/app/installations/{id}/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		authenticate(r)
		server.tokenRequests = append(server.tokenRequests, r.PathValue("id"))
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_minted", "expires_at": time.Now().Add(time.Hour)})
	})
	server.Server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestResolveAccessTokenMintsInstallationToken(t *testing.T) {
	key, encoded := generateAppKey(t)
	server := newAppServer(t, key, "12345")
	source := resource.Source{
		Repository: "halter/repo",
		AppID:      "12345",
		PrivateKey: encoded,
		V3Endpoint: server.URL,
		V4Endpoint: server.URL + "/graphql",
	}

	require.NoError(t, source.Validate())
	require.NoError(t, source.ResolveAccessToken())

	assert.Equal(t, "ghs_minted", source.AccessToken)
	assert.Equal(t, 1, server.installationLookups)
	assert.Equal(t, []string{"42"}, server.tokenRequests)
}

func TestResolveAccessTokenUsesConfiguredInstallation(t *testing.T) {
	key, encoded := generateAppKey(t)
	server := newAppServer(t, key, "12345")
	source := resource.Source{
		Repository:     "halter/repo",
		AppID:          "12345",
		PrivateKey:     encoded,
		InstallationID: "77",
		V3Endpoint:     server.URL,
		V4Endpoint:     server.URL + "/graphql",
	}

	require.NoError(t, source.ResolveAccessToken())

	assert.Equal(t, "ghs_minted", source.AccessToken)
	assert.Equal(t, 0, server.installationLookups)
	assert.Equal(t, []string{"77"}, server.tokenRequests)
}

func TestResolveAccessTokenLeavesAccessTokenSourcesAlone(t *testing.T) {
	source := resource.Source{Repository: "halter/repo", AccessToken: "ghp_static"}

	require.NoError(t, source.ResolveAccessToken())

	assert.Equal(t, "ghp_static", source.AccessToken)
}

func TestResolveAccessTokenRejectsUnparseableKey(t *testing.T) {
	source := resource.Source{Repository: "halter/repo", AppID: "12345", PrivateKey: "not a key"}

	err := source.ResolveAccessToken()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "private_key")
	assert.Empty(t, source.AccessToken)
}

func TestSourceAcceptsAppIDAsNumberOrString(t *testing.T) {
	for _, appID := range []string{`12345`, `"12345"`} {
		var source resource.Source
		require.NoError(t, json.Unmarshal([]byte(`{"repository":"halter/repo","private_key":"key","app_id":`+appID+`}`), &source))
		assert.Equal(t, "12345", source.AppID.String())
		assert.NoError(t, source.Validate())
	}
}

func TestValidateCredentials(t *testing.T) {
	tests := []struct {
		description string
		source      resource.Source
		wantErr     string
	}{
		{
			description: "access token alone",
			source:      resource.Source{Repository: "halter/repo", AccessToken: "ghp_static"},
		},
		{
			description: "app credentials alone",
			source:      resource.Source{Repository: "halter/repo", AppID: "12345", PrivateKey: "key"},
		},
		{
			description: "no credentials",
			source:      resource.Source{Repository: "halter/repo"},
			wantErr:     "access_token, or app_id and private_key, must be set",
		},
		{
			description: "app id without private key",
			source:      resource.Source{Repository: "halter/repo", AppID: "12345"},
			wantErr:     "app_id and private_key must be set together",
		},
		{
			description: "private key without app id",
			source:      resource.Source{Repository: "halter/repo", PrivateKey: "key"},
			wantErr:     "app_id and private_key must be set together",
		},
		{
			description: "access token and app credentials",
			source:      resource.Source{Repository: "halter/repo", AccessToken: "ghp_static", AppID: "12345", PrivateKey: "key"},
			wantErr:     "access_token cannot be set together with app_id and private_key",
		},
		{
			description: "installation id without app credentials",
			source:      resource.Source{Repository: "halter/repo", AccessToken: "ghp_static", InstallationID: "77"},
			wantErr:     "installation_id requires app_id and private_key",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := tc.source.Validate()
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, tc.wantErr)
		})
	}
}

func TestRedactSecretsCoversPrivateKeyPrintedAsJSON(t *testing.T) {
	_, encoded := generateAppKey(t)
	source := resource.Source{Repository: "halter/repo", AppID: "12345", PrivateKey: encoded}
	printed, err := json.Marshal(source)
	require.NoError(t, err)

	output := resource.RedactSecrets(source, string(printed)+"\n"+encoded)

	assert.NotContains(t, output, "PRIVATE KEY")
	assert.Contains(t, output, "REDACTED")
	assert.Contains(t, output, "halter/repo")
}
