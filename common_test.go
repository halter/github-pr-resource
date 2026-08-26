package resource_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	resource "github.com/telia-oss/github-pr-resource"
)

func TestRedactSecrets(t *testing.T) {
	source := resource.Source{
		AccessToken: "ghp_abc123secret",
		GitCryptKey: "cryptkey123",
		OdAdvanced: resource.OdAdvanced{
			VaultApproleSecretId: "vault-secret-id",
			DataDogApiKey:        "dd-api-key",
			DataDogAppKey:        "dd-app-key",
		},
	}

	input := `{"access_token":"ghp_abc123secret","git_crypt_key":"cryptkey123",` +
		`"vault_approle_secret_id":"vault-secret-id","datadog_api_key":"dd-api-key",` +
		`"datadog_app_key":"dd-app-key","repository":"halter/repo"}` +
		` fatal: unable to access 'https://x-oauth-basic:ghp_abc123secret@github.com/halter/repo'`

	output := resource.RedactSecrets(source, input)

	assert.NotContains(t, output, "ghp_abc123secret")
	assert.NotContains(t, output, "cryptkey123")
	assert.NotContains(t, output, "vault-secret-id")
	assert.NotContains(t, output, "dd-api-key")
	assert.NotContains(t, output, "dd-app-key")
	assert.Contains(t, output, "halter/repo")
	assert.Contains(t, output, "REDACTED")
}

func TestRedactSecretsEmptySource(t *testing.T) {
	input := "no secrets here"
	assert.Equal(t, input, resource.RedactSecrets(resource.Source{}, input))
}
