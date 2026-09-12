package operator

import (
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func effectiveSecretType(t corev1.SecretType) corev1.SecretType {
	if t == "" {
		return corev1.SecretTypeOpaque
	}
	return t
}

// requiredSecretKeys is deliberately a closed allowlist. Basic auth requires
// both keys, matching the operator contract (Kubernetes itself permits either).
func requiredSecretKeys(t corev1.SecretType) ([]string, error) {
	switch effectiveSecretType(t) {
	case corev1.SecretTypeOpaque:
		return nil, nil
	case corev1.SecretTypeDockerConfigJson:
		return []string{corev1.DockerConfigJsonKey}, nil
	case corev1.SecretTypeTLS:
		return []string{corev1.TLSCertKey, corev1.TLSPrivateKeyKey}, nil
	case corev1.SecretTypeBasicAuth:
		return []string{corev1.BasicAuthUsernameKey, corev1.BasicAuthPasswordKey}, nil
	case corev1.SecretTypeSSHAuth:
		return []string{corev1.SSHAuthPrivateKey}, nil
	default:
		return nil, fmt.Errorf("unsupported Secret type %q; allowed: Opaque, kubernetes.io/dockerconfigjson, kubernetes.io/tls, kubernetes.io/basic-auth, kubernetes.io/ssh-auth", t)
	}
}

func missingSecretKeys(required []string, data map[string][]byte) []string {
	var missing []string
	for _, key := range required {
		if _, ok := data[key]; !ok {
			missing = append(missing, key)
		}
	}
	return missing
}

// validateSecretData applies the API server's content checks for the allowed
// types and rejects null Docker configs, which are not credential objects.
// It never includes payloads or parser errors in status/events. TLS
// certificate correctness is the consumer's responsibility, as in Kubernetes.
func validateSecretData(t corev1.SecretType, data map[string][]byte) error {
	required, err := requiredSecretKeys(t)
	if err != nil {
		return err
	}
	if missing := missingSecretKeys(required, data); len(missing) != 0 {
		return fmt.Errorf("Secret type %q requires mapped data keys: %s", effectiveSecretType(t), strings.Join(missing, ", "))
	}
	switch effectiveSecretType(t) {
	case corev1.SecretTypeDockerConfigJson:
		var config map[string]json.RawMessage
		if err := json.Unmarshal(data[corev1.DockerConfigJsonKey], &config); err != nil || config == nil {
			return fmt.Errorf("Secret data key %q must contain a valid JSON object", corev1.DockerConfigJsonKey)
		}
	case corev1.SecretTypeSSHAuth:
		if len(data[corev1.SSHAuthPrivateKey]) == 0 {
			return fmt.Errorf("Secret data key %q must not be empty", corev1.SSHAuthPrivateKey)
		}
	}
	return nil
}
