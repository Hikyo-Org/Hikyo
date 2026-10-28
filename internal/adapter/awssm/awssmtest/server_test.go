package awssmtest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

func TestPutSecretValueMovesPreviousStage(t *testing.T) {
	s := New("123456789012", "eu-central-1")
	defer s.Close()
	call := func(operation string, input any, output any) {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Amz-Target", "secretsmanager."+operation)
		sum := sha256.Sum256(body)
		credentials := aws.Credentials{AccessKeyID: "AKIAHIKYOTEST0000001", SecretAccessKey: "synthetic-local-protocol"}
		if err := v4.NewSigner().SignHTTP(t.Context(), credentials, req, hex.EncodeToString(sum[:]), "secretsmanager", s.Region, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		resp, err := s.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d", operation, resp.StatusCode)
		}
		if output != nil {
			if err := json.NewDecoder(resp.Body).Decode(output); err != nil {
				t.Fatal(err)
			}
		}
	}
	first, second, staged, fourth := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64)
	call("CreateSecret", map[string]any{"Name": "stage-contract", "ClientRequestToken": first, "SecretString": "first synthetic"}, nil)
	assertStages := func(current, previous string) {
		t.Helper()
		var output struct {
			Stages map[string][]string `json:"VersionIdsToStages"`
		}
		call("DescribeSecret", map[string]string{"SecretId": "stage-contract"}, &output)
		for label, expected := range map[string]string{"AWSCURRENT": current, "AWSPREVIOUS": previous} {
			var owners []string
			for token, stages := range output.Stages {
				if slices.Contains(stages, label) {
					owners = append(owners, token)
				}
			}
			if len(owners) != 1 || owners[0] != expected {
				t.Fatalf("%s owners=%v, want %s", label, owners, expected)
			}
		}
	}
	// AWS moves AWSPREVIOUS whenever PutSecretValue moves AWSCURRENT.
	// https://docs.aws.amazon.com/secretsmanager/latest/apireference/API_PutSecretValue.html
	call("PutSecretValue", map[string]any{"SecretId": "stage-contract", "ClientRequestToken": second, "SecretString": "second synthetic"}, nil)
	assertStages(second, first)
	call("PutSecretValue", map[string]any{"SecretId": "stage-contract", "ClientRequestToken": second, "SecretString": "second synthetic"}, nil)
	assertStages(second, first)
	call("PutSecretValue", map[string]any{"SecretId": "stage-contract", "ClientRequestToken": staged, "SecretString": "staged synthetic", "VersionStages": []string{"HIKYO_PENDING"}}, nil)
	assertStages(second, first)
	call("PutSecretValue", map[string]any{"SecretId": "stage-contract", "ClientRequestToken": fourth, "SecretString": "fourth synthetic", "VersionStages": []string{"AWSCURRENT"}}, nil)
	assertStages(fourth, second)
}
