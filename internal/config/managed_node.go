package config

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const ManagedNewRootSourceKey = "HIKYO_NEW_ROOT_SOURCE"

const ManagedNodeOverridesKey = "HIKYO_NODE_OVERRIDES"
const MaxManagedNodeValueBytes = 65536

var managedNodeKeys = []string{
	"HIKYO_LISTEN", "HIKYO_OPERATIONAL_LISTEN", "HIKYO_PG_POOL_MAX", "HIKYO_ADMISSION_BUDGET_MIB", "HIKYO_BACKUP_DIR", "HIKYO_TRUSTED_PROXY_CIDRS",
	ManagedNewRootSourceKey, "HIKYO_TLS_CERT_PEM", "HIKYO_TLS_KEY_PEM", "HIKYO_ADAPTER_EGRESS_POLICY_JSON", "HIKYO_OIDC_EGRESS_POLICY_JSON", "HIKYO_DYNAMIC_EGRESS_POLICY_JSON",
	"HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE", "HIKYO_DEV_SERVICE_BUDGETS_DISABLED", "HIKYO_DEV_ADAPTER_FAKE_PROVIDER",
}

var managedNodeFiles = []struct{ source, target string }{
	{"HIKYO_TLS_CERT_FILE", "HIKYO_TLS_CERT_PEM"}, {"HIKYO_TLS_KEY_FILE", "HIKYO_TLS_KEY_PEM"},
	{"HIKYO_ADAPTER_EGRESS_POLICY_FILE", "HIKYO_ADAPTER_EGRESS_POLICY_JSON"},
	{"HIKYO_OIDC_EGRESS_POLICY_FILE", "HIKYO_OIDC_EGRESS_POLICY_JSON"},
	{"HIKYO_DYNAMIC_EGRESS_POLICY_FILE", "HIKYO_DYNAMIC_EGRESS_POLICY_JSON"},
}

// ManagedNodeKeys returns the closed ordinary-node setting allowlist. Bootstrap
// primary roots, databases and deployment identity are excluded. Development switches
// require the actual node to already run in development mode.
func ManagedNodeKeys() []string { return slices.Clone(managedNodeKeys) }

func managedNodeInputKeys() []string {
	keys := append(slices.Clone(managedNodeKeys[:6]), managedDevelopmentKeys...)
	for _, source := range managedNodeFiles {
		keys = append(keys, source.source)
	}
	return keys
}

// SeedNodeValues captures one node's effective settings and imports only its
// explicitly configured startup file sources. The caller aggregates each HA
// node separately; no source path is retained as an applied configuration value.
func (c *Config) SeedNodeValues() (map[string]string, error) {
	values := map[string]string{
		"HIKYO_LISTEN": c.Listen, "HIKYO_OPERATIONAL_LISTEN": c.OperationalListen,
		"HIKYO_ADMISSION_BUDGET_MIB": strconv.Itoa(c.AdmissionBudgetMiB),
		"HIKYO_TRUSTED_PROXY_CIDRS":  strings.Join(c.TrustedProxyCIDRs, ","),
	}
	if values["HIKYO_LISTEN"] == "" {
		values["HIKYO_LISTEN"] = "127.0.0.1:8080"
	}
	if values["HIKYO_OPERATIONAL_LISTEN"] == "" {
		values["HIKYO_OPERATIONAL_LISTEN"] = "127.0.0.1:8081"
	}
	if c.AdmissionBudgetMiB == 0 {
		values["HIKYO_ADMISSION_BUDGET_MIB"] = "272"
	}
	if c.Store.PostgresPoolMax > 0 {
		values["HIKYO_PG_POOL_MAX"] = strconv.FormatInt(int64(c.Store.PostgresPoolMax), 10)
	}
	if c.BackupDir != "" {
		values["HIKYO_BACKUP_DIR"] = c.BackupDir
	}
	if c.Dev {
		values["HIKYO_DEV_SERVICE_BUDGETS_DISABLED"] = strconv.FormatBool(c.DevServiceBudgetsDisabled)
		values["HIKYO_DEV_ADAPTER_FAKE_PROVIDER"] = strconv.FormatBool(c.DevAdapterFakeProvider)
		if c.DevAdmissionPerIPPerMinute != 0 {
			values["HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE"] = strconv.Itoa(c.DevAdmissionPerIPPerMinute)
		}
	} else if c.DevAdmissionPerIPPerMinute != 0 || c.DevServiceBudgetsDisabled || c.DevAdapterFakeProvider {
		return nil, errors.New("production configuration contains development-only controls")
	}
	for _, key := range append(slices.Clone(managedNodeKeys[:6]), managedDevelopmentKeys...) {
		if value, present := c.ManagedNodeInputs[key]; present {
			values[key] = value
		}
	}
	sources := maps.Clone(c.ManagedNodeInputs)
	if sources == nil {
		sources = make(map[string]string)
	}
	// Ordinary Load callers have parsed path descriptors and policy maps. The
	// bootstrap loader additionally retains raw egress file sources here.
	if c.TLSCertFile != "" {
		sources["HIKYO_TLS_CERT_FILE"] = c.TLSCertFile
	}
	if c.TLSKeyFile != "" {
		sources["HIKYO_TLS_KEY_FILE"] = c.TLSKeyFile
	}
	for _, source := range managedNodeFiles {
		if path := sources[source.source]; path != "" {
			content, err := readManagedSeedFileMode(path, source.source == "HIKYO_TLS_KEY_FILE")
			if err != nil {
				return nil, fmt.Errorf("%s: cannot import a regular file of at most 65536 bytes", source.source)
			}
			values[source.target] = content
		}
	}
	for key, content := range map[string]string{"HIKYO_TLS_CERT_PEM": c.TLSCertPEM, "HIKYO_TLS_KEY_PEM": c.TLSKeyPEM} {
		if content != "" {
			if _, present := values[key]; present {
				return nil, errors.New("multiple managed TLS content sources")
			}
			values[key] = content
		}
	}
	for key, policy := range map[string]map[string][]netip.Prefix{
		"HIKYO_ADAPTER_EGRESS_POLICY_JSON": c.AdapterEgressPolicy,
		"HIKYO_OIDC_EGRESS_POLICY_JSON":    c.OIDCEgressPolicy,
		"HIKYO_DYNAMIC_EGRESS_POLICY_JSON": c.DynamicEgressPolicy,
	} {
		if _, present := values[key]; present || policy == nil {
			continue
		}
		encoded := make(map[string][]string, len(policy))
		for origin, cidrs := range policy {
			encoded[origin] = []string{}
			for _, cidr := range cidrs {
				encoded[origin] = append(encoded[origin], cidr.String())
			}
		}
		raw, err := json.Marshal(encoded)
		if err != nil {
			return nil, errors.New("cannot serialize managed node policy")
		}
		values[key] = string(raw)
	}
	if _, err := parseManagedNodeValues(c, values); err != nil {
		return nil, err
	}
	return values, nil
}

// ApplyManagedNodeValues applies a complete node projection while retaining the
// supplied owner's effective policy. It performs no filesystem or network I/O.
func ApplyManagedNodeValues(base *Config, values map[string]string) (*Config, error) {
	if base == nil {
		return nil, errors.New("managed node configuration requires explicit bootstrap context")
	}
	return ApplyManagedOwnerAndNodeValues(base, base.ManagedOwnerValues(), values)
}

// ApplyManagedOwnerAndNodeValues validates one candidate's owner and local-node
// settings together, so a simultaneous destination/policy or TLS/listener change
// is judged against its own complete configuration rather than an older graph.
func ApplyManagedOwnerAndNodeValues(base *Config, ownerValues, nodeValues map[string]string) (*Config, error) {
	if base == nil {
		return nil, errors.New("managed node configuration requires explicit bootstrap context")
	}
	node, err := parseManagedNodeValues(base, nodeValues)
	if err != nil {
		return nil, err
	}
	return ApplyManagedOwnerValues(node, ownerValues)
}

// ValidateManagedNodeValues checks content and scalar syntax without assuming a
// deployment's database engine or development mode. Actual node admission must
// use ApplyManagedOwnerAndNodeValues with the real bootstrap context.
func ValidateManagedNodeValues(values map[string]string) error {
	_, err := parseManagedNodeValues(&Config{Dev: true, Store: Datastore{Engine: EnginePostgres}}, values)
	return err
}

func parseManagedNodeValues(base *Config, values map[string]string) (*Config, error) {
	for key, value := range values {
		if !slices.Contains(managedNodeKeys, key) {
			return nil, errors.New("managed node configuration contains an unsupported key")
		}
		if !utf8.ValidString(value) || len(value) > MaxManagedNodeValueBytes || (value == "" && key != "HIKYO_TRUSTED_PROXY_CIDRS") {
			return nil, fmt.Errorf("%s: invalid managed node value length", key)
		}
	}
	for _, key := range []string{"HIKYO_LISTEN", "HIKYO_OPERATIONAL_LISTEN", "HIKYO_ADMISSION_BUDGET_MIB"} {
		if values[key] == "" {
			return nil, fmt.Errorf("%s: every node must declare this effective setting", key)
		}
	}
	node := *base
	// A complete managed projection never revives the startup candidate path.
	node.NewRootKeyFile = ""
	node.NewRootSource = values[ManagedNewRootSourceKey]
	if node.NewRootSource != "" && !ValidManagedNodeID(node.NewRootSource) {
		return nil, errors.New("HIKYO_NEW_ROOT_SOURCE: expected an enrolled source alias")
	}
	for _, key := range managedDevelopmentKeys {
		if _, present := values[key]; present && !base.Dev {
			return nil, fmt.Errorf("%s: requires an already-development node", key)
		}
	}
	if err := parseDevelopmentOverrides(&node, func(key string) string { return values[key] }); err != nil {
		return nil, err
	}
	node.ManagedInputs, node.ManagedNodeInputs = maps.Clone(base.ManagedInputs), maps.Clone(base.ManagedNodeInputs)
	node.Listen, node.OperationalListen = values["HIKYO_LISTEN"], values["HIKYO_OPERATIONAL_LISTEN"]
	for _, key := range []string{"HIKYO_LISTEN", "HIKYO_OPERATIONAL_LISTEN"} {
		_, port, err := net.SplitHostPort(values[key])
		number, portErr := strconv.Atoi(port)
		if err != nil || portErr != nil || number < 0 || number > 65535 || (!base.Dev && number == 0) {
			return nil, fmt.Errorf("%s: invalid TCP listen address", key)
		}
	}
	if node.Listen == node.OperationalListen {
		return nil, errors.New("managed public and operational listeners must differ")
	}
	budget, err := strconv.ParseInt(values["HIKYO_ADMISSION_BUDGET_MIB"], 10, 32)
	if err != nil || budget < 1 || budget > math.MaxInt32 {
		return nil, errors.New("HIKYO_ADMISSION_BUDGET_MIB: invalid positive portable integer")
	}
	node.AdmissionBudgetMiB = int(budget)
	node.Store.PostgresPoolMax = 0
	if raw := values["HIKYO_PG_POOL_MAX"]; raw != "" {
		pool, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || pool < 1 || base.Store.Engine != EnginePostgres {
			return nil, errors.New("HIKYO_PG_POOL_MAX: requires a positive pool size and PostgreSQL")
		}
		node.Store.PostgresPoolMax = int32(pool)
	}
	node.BackupDir = values["HIKYO_BACKUP_DIR"]
	if strings.ContainsRune(node.BackupDir, 0) {
		return nil, errors.New("HIKYO_BACKUP_DIR: invalid destination")
	}
	if raw, present := values["HIKYO_TRUSTED_PROXY_CIDRS"]; present {
		node.TrustedProxyCIDRs, err = parseTrustedProxyCIDRs(raw)
		if err != nil {
			return nil, errors.New("HIKYO_TRUSTED_PROXY_CIDRS: invalid managed proxy policy")
		}
	} else {
		// A complete node projection never inherits bootstrap or another node trust.
		node.TrustedProxyCIDRs = nil
	}
	node.TLSCertFile, node.TLSKeyFile = "", ""
	node.TLSCertPEM, node.TLSKeyPEM = values["HIKYO_TLS_CERT_PEM"], values["HIKYO_TLS_KEY_PEM"]
	if err := validateManagedTLS(node.TLSCertPEM, node.TLSKeyPEM); err != nil {
		return nil, err
	}
	node.AdapterEgressPolicy, err = parseManagedEgress(values["HIKYO_ADAPTER_EGRESS_POLICY_JSON"], "adapter")
	if err != nil {
		return nil, err
	}
	node.OIDCEgressPolicy, err = parseManagedEgress(values["HIKYO_OIDC_EGRESS_POLICY_JSON"], "oidc")
	if err != nil {
		return nil, err
	}
	node.DynamicEgressPolicy, err = parseManagedEgress(values["HIKYO_DYNAMIC_EGRESS_POLICY_JSON"], "dynamic")
	if err != nil {
		return nil, err
	}
	return &node, nil
}

func validateManagedTLS(cert, key string) error {
	if cert == "" && key == "" {
		return nil
	}
	refusal := errors.New("managed TLS requires a valid certificate-only chain and matching private key")
	if cert == "" || key == "" {
		return refusal
	}
	remaining := []byte(cert)
	for len(bytes.TrimSpace(remaining)) > 0 {
		if !bytes.HasPrefix(bytes.TrimSpace(remaining), []byte("-----BEGIN CERTIFICATE-----")) {
			return refusal
		}
		block, rest := pem.Decode(remaining)
		if block == nil || block.Type != "CERTIFICATE" {
			return refusal
		}
		remaining = rest
	}
	if !strings.HasPrefix(strings.TrimSpace(key), "-----BEGIN ") {
		return refusal
	}
	block, rest := pem.Decode([]byte(key))
	if block == nil || len(bytes.TrimSpace(rest)) != 0 || !slices.Contains([]string{"PRIVATE KEY", "EC PRIVATE KEY", "RSA PRIVATE KEY"}, block.Type) {
		return refusal
	}
	if _, err := tls.X509KeyPair([]byte(cert), []byte(key)); err != nil {
		return refusal
	}
	return nil
}

func parseManagedEgress(raw, kind string) (map[string][]netip.Prefix, error) {
	if raw == "" {
		return nil, nil
	}
	if err := validateManagedJSON(raw); err != nil {
		return nil, errors.New("managed egress policy contains invalid JSON")
	}
	var shape map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &shape); err != nil || shape == nil {
		return nil, errors.New("managed egress policy must be an origin-to-CIDR-array object")
	}
	for _, entry := range shape {
		value := bytes.TrimSpace(entry)
		if len(value) == 0 || value[0] != '[' {
			return nil, errors.New("managed egress entries must be CIDR arrays")
		}
	}
	var policy map[string][]netip.Prefix
	var err error
	switch kind {
	case "adapter":
		policy, err = parseOriginEgressPolicy([]byte(raw), "HIKYO_ADAPTER_EGRESS_POLICY_JSON")
	case "oidc":
		policy, err = parseOIDCEgressPolicy([]byte(raw))
	case "dynamic":
		policy, err = parseDynamicEgressPolicy([]byte(raw))
	default:
		return nil, errors.New("unknown managed egress policy kind")
	}
	if err != nil || policy == nil {
		return nil, errors.New("managed egress policy contains an invalid origin or CIDR")
	}
	return policy, nil
}

// validateManagedJSON uses the standard JSON tokenizer to reject duplicate
// object names at every level. Unmarshal alone silently accepts duplicates.
func validateManagedJSON(raw string) error {
	if !utf8.ValidString(raw) || len(raw) == 0 || len(raw) > MaxManagedNodeValueBytes {
		return errors.New("invalid managed JSON size")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 16 {
			return errors.New("managed JSON nesting limit")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, compound := token.(json.Delim)
		if !compound {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]bool)
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := token.(string)
				if !ok || seen[name] {
					return errors.New("duplicate managed JSON member")
				}
				seen[name] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid managed JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing managed JSON input")
	}
	return nil
}
