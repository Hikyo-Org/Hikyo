package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// AWS Secrets Manager contract (#158). Names and values are refused, never
// rewritten: an operator sees the exact key that cannot be represented.
const (
	AWSSecretNameLimit  = 512
	AWSSecretValueLimit = 65536
	awsKMSKeyLimit      = 255
)

var (
	awsSecretName = regexp.MustCompile(`^[A-Za-z0-9/_+=.@-]+$`)
	// AWS appends "-" and six random characters to every secret ARN. A name
	// that already ends that way makes partial-ARN lookups ambiguous, so AWS
	// documents it as unsafe; Hikyo refuses it rather than guess.
	awsARNSuffix  = regexp.MustCompile(`-[A-Za-z0-9]{6}$`)
	awsAccountID  = regexp.MustCompile(`^[0-9]{12}$`)
	awsKMSKeySpec = regexp.MustCompile(`^[A-Za-z0-9:/_-]+$`)
)

// ValidateAWSSecretsManagerDestination checks the routing fields alone:
// the 12-digit account, the mode-specific secret name or path, and the
// optional customer KMS key (carried in Destination.Environment).
func ValidateAWSSecretsManagerDestination(destination Destination) error {
	if !awsAccountID.MatchString(destination.Owner) {
		return fmt.Errorf("aws-secrets-manager: destination owner must be the 12-digit AWS account id")
	}
	if destination.Visibility != "" || len(destination.SelectedRepositoryIDs) != 0 || destination.RepositoryID != 0 {
		return fmt.Errorf("aws-secrets-manager: repository visibility routing does not apply")
	}
	if kms := destination.Environment; kms != "" && (len(kms) > awsKMSKeyLimit || !awsKMSKeySpec.MatchString(kms)) {
		return fmt.Errorf("aws-secrets-manager: KMS key must be a key id, key ARN, alias name, or alias ARN")
	}
	switch destination.Kind {
	case JSONObject:
		if destination.Name == "" || strings.HasSuffix(destination.Name, "/") {
			return fmt.Errorf("aws-secrets-manager: json-object destination requires a secret name that does not end in /")
		}
		return validateAWSSecretName(destination.Name)
	case PerKey:
		if destination.Name == "" {
			return nil
		}
		if !strings.HasSuffix(destination.Name, "/") {
			return fmt.Errorf("aws-secrets-manager: per-key destination name is a path prefix and must end in /")
		}
		if !awsSecretName.MatchString(destination.Name) {
			return fmt.Errorf("aws-secrets-manager: path prefix %q uses characters outside A-Z a-z 0-9 / _ + = . @ -", destination.Name)
		}
		return nil
	default:
		return fmt.Errorf("aws-secrets-manager: destination kind must be json-object or per-key")
	}
}

func validateAWSSecretName(name string) error {
	switch {
	case name == "" || len(name) > AWSSecretNameLimit:
		return fmt.Errorf("aws-secrets-manager: secret name %q must be 1-%d bytes", name, AWSSecretNameLimit)
	case !awsSecretName.MatchString(name):
		return fmt.Errorf("aws-secrets-manager: secret name %q uses characters outside A-Z a-z 0-9 / _ + = . @ -", name)
	case awsARNSuffix.MatchString(name):
		return fmt.Errorf("aws-secrets-manager: secret name %q ends in a hyphen and six characters, which AWS ARN lookups confuse with its random suffix", name)
	}
	return nil
}

// ValidateAWSSecretsManagerManifest enforces the destination, name, and
// (when values is true) byte contract for one target.
func ValidateAWSSecretsManagerManifest(destination Destination, prefix string, entries []ManifestEntry, values bool) error {
	if err := ValidateAWSSecretsManagerDestination(destination); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		member := prefix + entry.CanonicalName
		switch {
		case entry.Classification != SecretClassification && entry.Classification != ConfigClassification:
			return fmt.Errorf("aws-secrets-manager: %s: unknown classification %q", entry.CanonicalName, entry.Classification)
		case !effectiveName.MatchString(member):
			return fmt.Errorf("aws-secrets-manager: %s: effective name %q is not uppercase identifier syntax", entry.CanonicalName, member)
		case values && !utf8.ValidString(entry.Value):
			return fmt.Errorf("aws-secrets-manager: %s: non-UTF-8 values cannot be stored byte-exactly as SecretString", entry.CanonicalName)
		case values && strings.ContainsRune(entry.Value, '\x00'):
			return fmt.Errorf("aws-secrets-manager: %s: NUL-containing values are refused", entry.CanonicalName)
		}
		normalized := strings.ToUpper(member)
		if _, ok := seen[normalized]; ok {
			return fmt.Errorf("aws-secrets-manager: %s: effective name %q collides case-insensitively", entry.CanonicalName, member)
		}
		seen[normalized] = struct{}{}
		if destination.Kind != PerKey {
			continue
		}
		if err := validateAWSSecretName(destination.Name + member); err != nil {
			return fmt.Errorf("aws-secrets-manager: %s: %w", entry.CanonicalName, err)
		}
		switch {
		case values && entry.Value == "":
			return fmt.Errorf("aws-secrets-manager: %s: AWS Secrets Manager does not store an empty SecretString; use json-object mode or a non-empty value", entry.CanonicalName)
		case values && len(entry.Value) > AWSSecretValueLimit:
			return fmt.Errorf("aws-secrets-manager: %s: value exceeds the %d-byte SecretString limit", entry.CanonicalName, AWSSecretValueLimit)
		}
	}
	if destination.Kind == JSONObject && values {
		document, err := AWSJSONDocument(prefix, entries)
		if err != nil {
			return err
		}
		if len(document) > AWSSecretValueLimit {
			return fmt.Errorf("aws-secrets-manager: json-object document for %q is %d bytes, over the %d-byte SecretString limit", destination.Name, len(document), AWSSecretValueLimit)
		}
	}
	return nil
}

// AWSJSONDocument is the deterministic json-object encoding: one member per
// key named prefix+canonical name, members sorted by name, no insignificant
// whitespace, and no HTML escaping. Values are JSON strings, so a consumer
// that parses the document gets the exact published bytes back.
func AWSJSONDocument(prefix string, entries []ManifestEntry) (string, error) {
	members := make(map[string]string, len(entries))
	for _, entry := range entries {
		name := prefix + entry.CanonicalName
		if _, ok := members[name]; ok {
			return "", fmt.Errorf("aws-secrets-manager: %s: json member %q is mapped twice", entry.CanonicalName, name)
		}
		members[name] = entry.Value
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	// encoding/json sorts map keys, which is what makes the document stable.
	if err := encoder.Encode(members); err != nil {
		return "", fmt.Errorf("aws-secrets-manager: encode json-object document: %w", err)
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}

// AWSDesiredRows maps a manifest onto the AWS secrets one target owns. In
// json-object mode the single row carries the encoded document and no key id.
func AWSDesiredRows(destination Destination, prefix string, manifest []ManifestEntry) ([]DesiredRow, error) {
	if destination.Kind == JSONObject {
		if len(manifest) == 0 {
			return nil, nil
		}
		document, err := AWSJSONDocument(prefix, manifest)
		if err != nil {
			return nil, err
		}
		return []DesiredRow{{
			ManifestEntry: ManifestEntry{Classification: SecretClassification, Value: document},
			Surface:       Secret, EffectiveName: destination.Name,
		}}, nil
	}
	rows := make([]DesiredRow, 0, len(manifest))
	for _, entry := range manifest {
		rows = append(rows, DesiredRow{ManifestEntry: entry, Surface: Secret, EffectiveName: destination.Name + prefix + entry.CanonicalName})
	}
	slices.SortFunc(rows, func(a, b DesiredRow) int { return strings.Compare(a.EffectiveName, b.EffectiveName) })
	return rows, nil
}

// MappedName is where one key lands on a target: the surface and the
// effective provider name. For a json-object target it is the JSON member
// inside the target's single secret.
func MappedName(provider string, destination Destination, prefix string, entry ManifestEntry) (Surface, string) {
	if Provider(provider) != AWSSecretsManagerProvider {
		return entry.Surface(), prefix + entry.CanonicalName
	}
	if destination.Kind == JSONObject {
		return Secret, prefix + entry.CanonicalName
	}
	return Secret, destination.Name + prefix + entry.CanonicalName
}

func awsClaims(destination Destination, prefix string, manifest []ManifestEntry) []Claim {
	if destination.Kind == JSONObject {
		return []Claim{{Surface: Secret, EffectiveName: destination.Name}}
	}
	out := make([]Claim, 0, len(manifest))
	for _, entry := range manifest {
		out = append(out, Claim{Surface: Secret, EffectiveName: destination.Name + prefix + entry.CanonicalName, KeyID: entry.KeyID})
	}
	return out
}

func renderAWSConsumption(destination Destination, prefix string, entries []ManifestEntry) string {
	rows := slices.Clone(entries)
	slices.SortFunc(rows, func(a, b ManifestEntry) int { return strings.Compare(a.CanonicalName, b.CanonicalName) })
	var out strings.Builder
	if destination.Kind == JSONObject {
		out.WriteString("# AWS Secrets Manager: one secret holding a JSON object; each member is one key.\n")
		_, _ = fmt.Fprintf(&out, "secret_id: %s\nmembers:\n", strconv.Quote(destination.Name))
		for _, entry := range rows {
			_, _ = fmt.Fprintf(&out, "  %s: %s\n", entry.CanonicalName, strconv.Quote(prefix+entry.CanonicalName))
		}
		return out.String()
	}
	out.WriteString("# AWS Secrets Manager: one secret per key; the value is the SecretString.\n")
	out.WriteString("secrets:\n")
	for _, entry := range rows {
		_, _ = fmt.Fprintf(&out, "  %s: %s\n", entry.CanonicalName, strconv.Quote(destination.Name+prefix+entry.CanonicalName))
	}
	return out.String()
}
