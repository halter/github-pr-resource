package resource

/**
Common functions
*/
import (
	"encoding/json"
	"log"
	"strings"
)

func RedactSecrets(s Source, str string) string {
	for _, secret := range []string{
		s.AccessToken,
		s.GitCryptKey,
		s.OdAdvanced.VaultApproleSecretId,
		s.OdAdvanced.DataDogApiKey,
		s.OdAdvanced.DataDogAppKey,
	} {
		if secret != "" {
			str = strings.ReplaceAll(str, secret, "REDACTED")
		}
	}
	return str
}

// print the request json coming in to "in|out|check"
func PrintDebugInput(s Source, obj any) {
	if s.OdAdvanced.Debug {
		jsonBytes, _ := json.Marshal(obj)
		log.Printf("input jsonStr : %s\n", RedactSecrets(s, string(jsonBytes)))
		log.Printf("Debig Tip1: run this docker image locally: docker run -it --entrypoint=/bin/sh opendoor/telia-oss-github-pr-resource:dev\n")
		log.Printf("Debug Tip2: save the above jsonStr to /tmp/request.json\n")
		log.Printf("Debug Tip3: cd /opt/resource && cat /tmp/request.json | <./in . |./out .|./check>\n")
	}
}

func PrintDebugOutput(s Source, obj any) {
	if s.OdAdvanced.Debug {
		jsonBytes, _ := json.Marshal(obj)
		log.Printf("output jsonStr : %s\n", RedactSecrets(s, string(jsonBytes)))
	}
}

func SayThanks() {
	log.Printf("Thanks for using Opendoor's https://github.com/opendoor-labs/github-pr-resource\n")
}
