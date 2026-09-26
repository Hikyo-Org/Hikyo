package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
)

// Private PKI (#154, docs/adr/pki.md). `pki issuer …` and `pki profile …` are
// instance administration (`instance-config`); `cert …` is the environment
// lifecycle. A CA private key enters only through --key-file or --key-stdin
// (never argv) and is never printed; a server-generated leaf key is written
// once through the print triad.

func pkiBase() string { return api.PathPrefix + "/instance/pki" }

func pkiIssuerTable(list apigen.PkiIssuerList) Table {
	rows := make([][]string, 0, len(list.Issuers))
	for _, i := range list.Issuers {
		hold := ""
		if i.RestoreHold {
			hold = "held"
		}
		rows = append(rows, []string{i.Name, strconv.FormatInt(i.Version, 10), string(i.Kind), string(i.State), hold,
			i.KeyAlgorithm, i.KeyFingerprint, timeOrDash(i.NotAfter), timeOrDash(i.CrlNextUpdate)})
	}
	return Table{
		// No key column: a CA private key never leaves the server.
		Columns: []string{"NAME", "VERSION", "KIND", "STATE", "HOLD", "ALGORITHM", "KEY FINGERPRINT", "NOT AFTER", "CRL NEXT UPDATE"},
		Rows:    rows,
		JSON:    list,
	}
}

func pkiProfileTable(list apigen.PkiProfileList) Table {
	rows := make([][]string, 0, len(list.Profiles))
	for _, p := range list.Profiles {
		bindings := make([]string, 0, len(p.Bindings))
		for _, b := range p.Bindings {
			scope := b.OrgId + "/" + b.ProjectId
			if b.EnvironmentId != nil {
				scope += "/" + *b.EnvironmentId
			}
			bindings = append(bindings, b.Id+"="+scope)
		}
		rows = append(rows, []string{p.Name, strings.Join(p.Policy.AllowedIssuers, ","),
			(time.Duration(p.Policy.MaxTtlSeconds) * time.Second).String(), strconv.FormatBool(p.Policy.MachineIssuance),
			strings.Join(bindings, " "), strconv.FormatInt(p.RowVersion, 10)})
	}
	return Table{
		Columns: []string{"NAME", "ISSUERS", "MAX TTL", "MACHINES", "BINDINGS", "ROW VERSION"},
		Rows:    rows,
		JSON:    list,
	}
}

func certificateTable(list apigen.CertificateList) Table {
	rows := make([][]string, 0, len(list.Certificates))
	for _, c := range list.Certificates {
		names := append(append(append([]string{}, c.DnsNames...), c.IpAddresses...), c.Uris...)
		rows = append(rows, []string{c.Id, c.Serial, c.Profile, c.IssuerName + " v" + strconv.FormatInt(c.IssuerVersion, 10),
			string(c.State), strings.Join(names, ","), c.NotAfter.UTC().Format(time.RFC3339)})
	}
	return Table{
		// No key column: a private key is never stored, so never listed.
		Columns: []string{"ID", "SERIAL", "PROFILE", "ISSUER", "STATE", "NAMES", "NOT AFTER"},
		Rows:    rows,
		JSON:    list,
	}
}

// pkiKeySource is protected input for CA key import: a file or stdin, never
// argv. PEM is multi-line, so this is not the one-line credential reader.
type pkiKeySource struct {
	stdin bool
	file  string
}

func (s pkiKeySource) set() bool { return s.stdin || s.file != "" }

func (s pkiKeySource) read(ios IO) ([]byte, error) {
	if s.stdin && s.file != "" {
		return nil, failf(ExitUsage, "--key-stdin and --key-file are mutually exclusive")
	}
	var raw []byte
	var err error
	if s.stdin {
		raw, err = io.ReadAll(io.LimitReader(ios.Stdin, 16385))
	} else {
		raw, err = os.ReadFile(s.file)
	}
	if err != nil {
		return nil, failf(ExitRefused, "reading the CA private key: %v", err)
	}
	if len(raw) > 16384 {
		crypto.Zero(raw)
		return nil, failf(ExitRefused, "the CA private key input exceeds 16384 bytes")
	}
	return raw, nil
}

// readPublicFile reads public material (certificates, CSRs, policies).
func readPublicFile(path, what string) (string, error) {
	if path == "" {
		return "", nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", failf(ExitUsage, "reading %s: %v", what, err)
	}
	return string(raw), nil
}

func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func runPKI(ctx context.Context, ios IO, args []string) error {
	noun, rest, err := subverb("pki", args, "issuer", "profile")
	if err != nil {
		return err
	}
	if noun == "issuer" {
		return runPKIIssuer(ctx, ios, rest)
	}
	return runPKIProfile(ctx, ios, rest)
}

func runPKIIssuer(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("pki issuer", args, "list", "show", "create-root", "create-intermediate", "import",
		"rotate", "install", "retire", "revoke", "reconcile", "crl")
	if err != nil {
		return err
	}
	var format, cn, org, alg, ttl, parent, crlURL, certFile, chainFile, csrOut string
	var version int64
	var publish bool
	var key pkiKeySource
	st, flags, err := parseCommon("pki issuer "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		switch sub {
		case "create-root", "create-intermediate", "import", "rotate":
			fs.StringVar(&crlURL, "crl-url", "", "public CRL distribution URL embedded in issued certificates")
		}
		switch sub {
		case "create-root", "create-intermediate":
			fs.StringVar(&cn, "common-name", "", "CA subject common name")
			fs.StringVar(&org, "organization", "", "CA subject organization")
		}
		switch sub {
		case "create-root", "create-intermediate", "rotate":
			fs.StringVar(&alg, "key-algorithm", "", "ecdsa-p256 (default), ecdsa-p384, rsa-3072 or rsa-4096")
			fs.StringVar(&ttl, "ttl", "", "CA certificate lifetime, e.g. 8760h")
		}
		if sub == "create-intermediate" {
			fs.StringVar(&parent, "parent", "", "sign with this Hikyo issuer; omit for a pending CSR an offline root signs")
		}
		if sub == "create-intermediate" || sub == "rotate" {
			fs.StringVar(&csrOut, "csr-out", "", "write a pending version's CSR to this file")
		}
		switch sub {
		case "import", "rotate", "install":
			fs.StringVar(&certFile, "cert-file", "", "PEM CA certificate")
			fs.StringVar(&chainFile, "chain-file", "", "PEM chain above the certificate, ending with the root")
		}
		if sub == "import" || sub == "rotate" {
			fs.StringVar(&key.file, "key-file", "", "read the CA private key (PEM) from this file")
			fs.BoolVar(&key.stdin, "key-stdin", false, "read the CA private key (PEM) from stdin")
		}
		switch sub {
		case "retire", "revoke", "crl":
			fs.Int64Var(&version, "version", 0, "issuer version")
		}
		if sub == "crl" {
			fs.BoolVar(&publish, "publish", false, "sign and publish a fresh CRL now")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	name := flags.positional()
	if sub == "list" {
		if err := flags.checkNoPositionals("pki issuer list"); err != nil {
			return err
		}
	} else if name == "" || len(flags.positionals) > 1 {
		return failf(ExitUsage, "pki issuer %s requires exactly one issuer name", sub)
	}
	var ttlSecs *int64
	if ttl != "" {
		secs, err := ttlSeconds(ttl)
		if err != nil {
			return err
		}
		ttlSecs = &secs
	}
	certPEM, err := readPublicFile(certFile, "the certificate")
	if err != nil {
		return err
	}
	chainPEM, err := readPublicFile(chainFile, "the chain")
	if err != nil {
		return err
	}
	var keyPEM []byte
	if key.set() {
		if keyPEM, err = key.read(ios); err != nil {
			return err
		}
		defer crypto.Zero(keyPEM)
	}
	client, _, err := authenticatedClient(st, ios, flags)
	if err != nil {
		return err
	}
	issuerPath := pkiBase() + "/issuers/" + url.PathEscape(name)
	one := func(out apigen.PkiIssuer) error {
		if out.CsrPem != nil && csrOut != "" {
			if err := os.WriteFile(csrOut, []byte(*out.CsrPem), 0o644); err != nil {
				return failf(ExitRefused, "writing the CSR: %v", err)
			}
		}
		table := pkiIssuerTable(apigen.PkiIssuerList{Issuers: []apigen.PkiIssuer{out}})
		table.JSON = out
		if err := Render(ios.Stdout, f, table); err != nil {
			return err
		}
		if out.CsrPem != nil && csrOut == "" && f == FormatTable {
			fmt.Fprintf(ios.Stdout, "\nPending: sign this CSR with the offline root, then run `hikyo pki issuer install %s`.\n%s", out.Name, *out.CsrPem)
		}
		return nil
	}
	versionPath := func() (string, error) {
		if version <= 0 {
			return "", failf(ExitUsage, "pki issuer %s requires --version", sub)
		}
		return issuerPath + "/versions/" + strconv.FormatInt(version, 10), nil
	}
	switch sub {
	case "list", "show", "reconcile":
		var out apigen.PkiIssuerList
		method, path := http.MethodGet, pkiBase()+"/issuers"
		if sub == "show" {
			path = issuerPath
		} else if sub == "reconcile" {
			method, path = http.MethodPost, issuerPath+"/reconcile"
		}
		if err := client.Do(ctx, method, path, nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, pkiIssuerTable(out))
	case "create-root", "create-intermediate", "import":
		mode := map[string]apigen.PkiIssuerCreateRequestMode{"create-root": "root", "create-intermediate": "intermediate", "import": "import"}[sub]
		body := apigen.PkiIssuerCreateRequest{
			Mode: mode, Name: name, CommonName: nonEmptyPtr(cn), Organization: nonEmptyPtr(org),
			TtlSeconds: ttlSecs, Parent: nonEmptyPtr(parent), CrlDistributionUrl: nonEmptyPtr(crlURL),
			CertificatePem: nonEmptyPtr(certPEM), ChainPem: nonEmptyPtr(chainPEM),
		}
		if alg != "" {
			a := apigen.PkiIssuerCreateRequestKeyAlgorithm(alg)
			body.KeyAlgorithm = &a
		}
		if sub == "import" {
			if !key.set() || certPEM == "" {
				return failf(ExitUsage, "pki issuer import requires --key-file or --key-stdin, and --cert-file")
			}
			keyText := string(keyPEM)
			body.PrivateKeyPem = &keyText
		}
		var out apigen.PkiIssuer
		if err := client.Do(ctx, http.MethodPost, pkiBase()+"/issuers", body, &out); err != nil {
			return err
		}
		return one(out)
	case "rotate":
		body := apigen.PkiIssuerRotateRequest{TtlSeconds: ttlSecs, CrlDistributionUrl: nonEmptyPtr(crlURL),
			CertificatePem: nonEmptyPtr(certPEM), ChainPem: nonEmptyPtr(chainPEM)}
		if alg != "" {
			a := apigen.PkiIssuerRotateRequestKeyAlgorithm(alg)
			body.KeyAlgorithm = &a
		}
		if key.set() {
			keyText := string(keyPEM)
			body.PrivateKeyPem = &keyText
		}
		var out apigen.PkiIssuer
		if err := client.Do(ctx, http.MethodPost, issuerPath+"/rotate", body, &out); err != nil {
			return err
		}
		return one(out)
	case "install":
		if certPEM == "" || chainPEM == "" {
			return failf(ExitUsage, "pki issuer install requires --cert-file and --chain-file")
		}
		var out apigen.PkiIssuer
		if err := client.Do(ctx, http.MethodPost, issuerPath+"/install", apigen.PkiIssuerInstallRequest{CertificatePem: certPEM, ChainPem: chainPEM}, &out); err != nil {
			return err
		}
		return one(out)
	case "retire", "revoke":
		path, err := versionPath()
		if err != nil {
			return err
		}
		var out apigen.PkiIssuer
		if err := client.Do(ctx, http.MethodPost, path+"/"+sub, nil, &out); err != nil {
			return err
		}
		return one(out)
	case "crl":
		path, err := versionPath()
		if err != nil {
			return err
		}
		if publish {
			var out apigen.PkiIssuer
			if err := client.Do(ctx, http.MethodPost, path+"/crl", nil, &out); err != nil {
				return err
			}
		}
		var out apigen.PkiCrl
		if err := client.Do(ctx, http.MethodGet, path+"/crl", nil, &out); err != nil {
			return err
		}
		return renderCRL(ios, f, out)
	}
	return failf(ExitUsage, "unknown pki issuer verb %q", sub)
}

func renderCRL(ios IO, f Format, out apigen.PkiCrl) error {
	if f == FormatJSON {
		return Render(ios.Stdout, f, Table{JSON: out})
	}
	_, err := io.WriteString(ios.Stdout, out.CrlPem)
	return err
}

func readPolicy(path string) (apigen.PkiPolicy, error) {
	raw, err := readPublicFile(path, "the policy file")
	if err != nil {
		return apigen.PkiPolicy{}, err
	}
	if raw == "" {
		return apigen.PkiPolicy{}, failf(ExitUsage, "--policy-file is required")
	}
	var policy apigen.PkiPolicy
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return apigen.PkiPolicy{}, failf(ExitUsage, "the policy file is not a PkiPolicy: %v", err)
	}
	return policy, nil
}

func runPKIProfile(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("pki profile", args, "list", "show", "create", "update", "delete", "bind", "unbind")
	if err != nil {
		return err
	}
	var format, policyFile string
	var rowVersion int64
	st, flags, err := parseCommon("pki profile "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "create" || sub == "update" {
			fs.StringVar(&policyFile, "policy-file", "", "JSON policy (the PkiPolicy shape)")
		}
		if sub == "update" {
			fs.Int64Var(&rowVersion, "row-version", 0, "refuse if the profile changed since this version")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	name := flags.positional()
	switch {
	case sub == "list":
		if err := flags.checkNoPositionals("pki profile list"); err != nil {
			return err
		}
	case sub == "unbind":
		if len(flags.positionals) != 2 {
			return failf(ExitUsage, "pki profile unbind requires a profile name and a binding id")
		}
	case name == "" || len(flags.positionals) > 1:
		return failf(ExitUsage, "pki profile %s requires exactly one profile name", sub)
	}
	var policy apigen.PkiPolicy
	if sub == "create" || sub == "update" {
		if policy, err = readPolicy(policyFile); err != nil {
			return err
		}
	}
	var client *Client
	var resolved Resolved
	if sub == "bind" {
		client, _, resolved, err = authenticatedTarget(st, ios, flags)
	} else {
		client, _, err = authenticatedClient(st, ios, flags)
	}
	if err != nil {
		return err
	}
	profilePath := pkiBase() + "/profiles/" + url.PathEscape(name)
	one := func(out apigen.PkiProfile) error {
		table := pkiProfileTable(apigen.PkiProfileList{Profiles: []apigen.PkiProfile{out}})
		table.JSON = out
		return Render(ios.Stdout, f, table)
	}
	var out apigen.PkiProfile
	switch sub {
	case "list":
		var list apigen.PkiProfileList
		if err := client.Do(ctx, http.MethodGet, pkiBase()+"/profiles", nil, &list); err != nil {
			return err
		}
		return Render(ios.Stdout, f, pkiProfileTable(list))
	case "show":
		err = client.Do(ctx, http.MethodGet, profilePath, nil, &out)
	case "create":
		err = client.Do(ctx, http.MethodPost, pkiBase()+"/profiles", apigen.PkiProfileCreateRequest{Name: name, Policy: policy}, &out)
	case "update":
		body := apigen.PkiProfileUpdateRequest{Policy: policy}
		if rowVersion > 0 {
			body.RowVersion = &rowVersion
		}
		err = client.Do(ctx, http.MethodPut, profilePath, body, &out)
	case "delete":
		return client.Do(ctx, http.MethodDelete, profilePath, nil, nil)
	case "bind":
		org, err := resolved.Require(DimOrg)
		if err != nil {
			return err
		}
		project, err := resolved.Require(DimProject)
		if err != nil {
			return err
		}
		body := apigen.PkiProfileBindRequest{OrgId: org, ProjectId: project}
		// Only an explicit --env narrows a binding: an environment an ambient
		// context happens to select must not silently narrow a project-wide
		// binding, or widen it later when the context changes.
		if flags.Env != "" {
			env := resolved.Get(DimEnv)
			body.EnvironmentId = &env
		}
		if err := client.Do(ctx, http.MethodPost, profilePath+"/bindings", body, &out); err != nil {
			return err
		}
		return one(out)
	case "unbind":
		err = client.Do(ctx, http.MethodDelete, profilePath+"/bindings/"+url.PathEscape(flags.positionals[1]), nil, &out)
	}
	if err != nil {
		return err
	}
	return one(out)
}

func runCert(ctx context.Context, ios IO, args []string) (returnErr error) {
	sub, rest, err := subverb("cert", args, "profiles", "list", "show", "issue", "renew", "revoke", "crl")
	if err != nil {
		return err
	}
	var format, profile, issuer, csrFile, alg, cn, ttl, certOut, outputFile, reason string
	var generate, dangerous, pem bool
	var dns, ips, uris stringList
	st, flags, err := parseCommon("cert "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "issue" {
			fs.StringVar(&profile, "profile", "", "certificate profile bound to the environment")
			fs.StringVar(&issuer, "issuer", "", "issuer the profile allows (default: the first with an active version)")
			fs.StringVar(&csrFile, "csr-file", "", "PEM CSR (only its public key is used)")
			fs.BoolVar(&generate, "generate-key", false, "have the server generate the key; it is shown exactly once")
			fs.StringVar(&alg, "key-algorithm", "", "algorithm for --generate-key (default ecdsa-p256)")
			fs.StringVar(&cn, "common-name", "", "subject common name (must be one of the --dns names)")
			fs.Var(&dns, "dns", "DNS subject alternative name (repeatable)")
			fs.Var(&ips, "ip", "IP subject alternative name (repeatable)")
			fs.Var(&uris, "uri", "URI subject alternative name, e.g. a SPIFFE ID (repeatable)")
			fs.StringVar(&ttl, "ttl", "", "certificate lifetime, e.g. 24h (default: the profile's)")
			fs.StringVar(&outputFile, "output-file", "", "write the generated private key to a fresh 0600 file")
			fs.BoolVar(&dangerous, "dangerously-print", false, "write the generated private key to stdout (and whatever collects it)")
		}
		if sub == "issue" || sub == "renew" || sub == "show" {
			fs.StringVar(&certOut, "cert-out", "", "write the certificate and its chain (PEM) to this file")
		}
		if sub == "show" {
			fs.BoolVar(&pem, "pem", false, "print the certificate and its chain as PEM")
		}
		if sub == "revoke" {
			fs.StringVar(&reason, "reason", "", "unspecified, key-compromise, affiliation-changed, superseded, cessation-of-operation or privilege-withdrawn")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	id := flags.positional()
	switch sub {
	case "profiles", "list", "issue":
		if err := flags.checkNoPositionals("cert " + sub); err != nil {
			return err
		}
	default:
		if id == "" || len(flags.positionals) > 1 {
			return failf(ExitUsage, "cert %s requires exactly one certificate id", sub)
		}
	}
	var csrPEM string
	var sink *disclose.PreparedSink
	if sub == "issue" {
		if profile == "" || generate == (csrFile != "") {
			return failf(ExitUsage, "cert issue requires --profile and exactly one of --csr-file or --generate-key")
		}
		if csrPEM, err = readPublicFile(csrFile, "the CSR"); err != nil {
			return err
		}
		if generate {
			// Prepare the display-once sink BEFORE any network call, so the
			// reserved destination is the exact destination the key goes to.
			sink, err = ios.prepareDisclosure(disclose.Options{OutputFile: outputFile, DangerouslyPrint: dangerous, Stdout: ios.Stdout})
			if err != nil {
				return failf(ExitRefused, "the private key has nowhere to go: %v", err)
			}
			defer sink.AbortOnReturn(&returnErr)
		}
	}
	client, _, resolved, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return err
	}
	org, err := resolved.Require(DimOrg)
	if err != nil {
		return err
	}
	project, err := resolved.Require(DimProject)
	if err != nil {
		return err
	}
	env, err := resolved.Require(DimEnv)
	if err != nil {
		return err
	}
	envBase := adapterBase(org, project) + "/environments/" + url.PathEscape(env)
	base := envBase + "/certificates"
	writeCert := func(c apigen.Certificate) error {
		if certOut == "" || c.CertificatePem == nil {
			return nil
		}
		content := *c.CertificatePem
		if c.ChainPem != nil {
			content += *c.ChainPem
		}
		if err := os.WriteFile(certOut, []byte(content), 0o644); err != nil {
			return failf(ExitRefused, "writing the certificate: %v", err)
		}
		return nil
	}
	one := func(c apigen.Certificate) error {
		if err := writeCert(c); err != nil {
			return err
		}
		table := certificateTable(apigen.CertificateList{Certificates: []apigen.Certificate{c}})
		table.JSON = c
		return Render(ios.Stdout, f, table)
	}
	switch sub {
	case "profiles":
		var out apigen.CertificateProfileList
		if err := client.Do(ctx, http.MethodGet, envBase+"/certificate-profiles", nil, &out); err != nil {
			return err
		}
		rows := make([][]string, 0, len(out.Profiles))
		for _, p := range out.Profiles {
			patterns := append(append(append([]string{}, p.Policy.DnsPatterns...), p.Policy.IpRanges...), p.Policy.UriPatterns...)
			rows = append(rows, []string{p.Name, strings.Join(patterns, ","), (time.Duration(p.Policy.MaxTtlSeconds) * time.Second).String()})
		}
		return Render(ios.Stdout, f, Table{Columns: []string{"PROFILE", "NAMES", "MAX TTL"}, Rows: rows, JSON: out})
	case "list":
		var out apigen.CertificateList
		if err := client.Do(ctx, http.MethodGet, base, nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, certificateTable(out))
	case "show":
		var out apigen.Certificate
		if err := client.Do(ctx, http.MethodGet, base+"/"+url.PathEscape(id), nil, &out); err != nil {
			return err
		}
		if pem && out.CertificatePem != nil {
			if err := writeCert(out); err != nil {
				return err
			}
			content := *out.CertificatePem
			if out.ChainPem != nil {
				content += *out.ChainPem
			}
			_, err := io.WriteString(ios.Stdout, content)
			return err
		}
		return one(out)
	case "renew", "revoke":
		var body any
		if sub == "revoke" && reason != "" {
			r := apigen.CertificateRevokeRequestReason(reason)
			body = apigen.CertificateRevokeRequest{Reason: &r}
		}
		var out apigen.Certificate
		if err := client.Do(ctx, http.MethodPost, base+"/"+url.PathEscape(id)+"/"+sub, body, &out); err != nil {
			return err
		}
		return one(out)
	case "crl":
		var out apigen.PkiCrl
		if err := client.Do(ctx, http.MethodGet, base+"/"+url.PathEscape(id)+"/crl", nil, &out); err != nil {
			return err
		}
		return renderCRL(ios, f, out)
	case "issue":
		body := apigen.CertificateIssueRequest{Profile: profile, Issuer: nonEmptyPtr(issuer), CsrPem: nonEmptyPtr(csrPEM), CommonName: nonEmptyPtr(cn)}
		if generate {
			body.GenerateKey = &generate
			if alg != "" {
				a := apigen.CertificateIssueRequestKeyAlgorithm(alg)
				body.KeyAlgorithm = &a
			}
		}
		if len(dns) > 0 {
			names := []string(dns)
			body.DnsNames = &names
		}
		if len(ips) > 0 {
			addresses := []string(ips)
			body.IpAddresses = &addresses
		}
		if len(uris) > 0 {
			values := []string(uris)
			body.Uris = &values
		}
		if ttl != "" {
			secs, err := ttlSeconds(ttl)
			if err != nil {
				return err
			}
			body.TtlSeconds = &secs
		}
		var out apigen.CertificateIssueResult
		if err := client.Do(ctx, http.MethodPost, base, body, &out); err != nil {
			return err
		}
		if generate {
			if out.PrivateKeyPem == nil {
				return failf(ExitInternal, "the server issued a certificate but returned no generated key")
			}
			if _, err := sink.WriteOnce("hikyo certificate private key (shown once)", *out.PrivateKeyPem); err != nil {
				return failf(ExitRefused, "disclosing the private key: %v", err)
			}
		}
		return one(out.Certificate)
	}
	return failf(ExitUsage, "unknown cert verb %q", sub)
}
