package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
)

// SSH user certificates (#155). The CA private key is write-only (read from
// stdin or a file, never argv) and never returned; a generated user private
// key is disclosed once through the print triad.

func sshCATable(list apigen.SSHCAList) Table {
	rows := make([][]string, 0, len(list.Items))
	for _, ca := range list.Items {
		for _, k := range ca.Keys {
			trusted := "no"
			if k.Trusted {
				trusted = "yes"
			}
			until := "-"
			if k.RetireAfter != nil {
				until = k.RetireAfter.UTC().Format(time.RFC3339)
			}
			rows = append(rows, []string{string(ca.Id), ca.Name, string(k.Id), string(k.Algorithm), string(k.State), trusted, until, k.Fingerprint})
		}
	}
	return Table{Columns: []string{"CA", "NAME", "KEY", "ALGORITHM", "STATE", "TRUSTED", "TRUSTED UNTIL", "FINGERPRINT"}, Rows: rows, JSON: list}
}

func sshProfileTable(list apigen.SSHProfileList) Table {
	rows := make([][]string, 0, len(list.Items))
	for _, p := range list.Items {
		state := "enabled"
		if !p.Enabled {
			state = "disabled"
		}
		rows = append(rows, []string{
			string(p.Id), p.Name, string(p.CaId), strings.Join(p.Principals, ","),
			(time.Duration(p.MaxTtlSeconds) * time.Second).String(), fmt.Sprint(len(p.Requesters)), state,
		})
	}
	return Table{Columns: []string{"ID", "NAME", "CA", "PRINCIPALS", "MAX TTL", "REQUESTERS", "STATE"}, Rows: rows, JSON: list}
}

func sshCertificateTable(list apigen.SSHCertificateList) Table {
	rows := make([][]string, 0, len(list.Items))
	for _, c := range list.Items {
		krl := "no"
		if c.InKrl {
			krl = "yes"
		}
		rows = append(rows, []string{
			string(c.Id), c.Serial, strings.Join(c.Principals, ","), string(c.RequesterPrincipalId),
			c.ValidBefore.UTC().Format(time.RFC3339), string(c.Status), krl,
		})
	}
	// No key column: a generated private key is disclosed once at issue and
	// never stored or read back.
	return Table{Columns: []string{"ID", "SERIAL", "PRINCIPALS", "REQUESTER", "VALID BEFORE", "STATUS", "IN KRL"}, Rows: rows, JSON: list}
}

// readSSHPrivateKey reads a PEM private key from stdin or a file. It never
// takes the key from argv.
func readSSHPrivateKey(ios IO, stdin bool, file string) (string, error) {
	if stdin && file != "" {
		return "", failf(ExitUsage, "--stdin and --key-file are mutually exclusive")
	}
	var reader io.Reader
	switch {
	case stdin:
		reader = ios.Stdin
	case file != "":
		f, err := os.Open(file)
		if err != nil {
			return "", failf(ExitRefused, "reading the private key: %v", err)
		}
		defer f.Close()
		reader = f
	default:
		return "", nil
	}
	raw, err := io.ReadAll(io.LimitReader(reader, 16<<10+1))
	if err != nil {
		return "", failf(ExitRefused, "reading the private key: %v", err)
	}
	if len(raw) == 0 || len(raw) > 16<<10 {
		return "", failf(ExitRefused, "the private key must be a single PEM block of at most 16 KiB")
	}
	return string(raw), nil
}

type sshEnvTarget struct {
	client *Client
	base   string
}

func sshTarget(st *State, ios IO, flags commonFlags) (sshEnvTarget, error) {
	client, _, resolved, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return sshEnvTarget{}, err
	}
	org, err := resolved.Require(DimOrg)
	if err != nil {
		return sshEnvTarget{}, err
	}
	project, err := resolved.Require(DimProject)
	if err != nil {
		return sshEnvTarget{}, err
	}
	env, err := resolved.Require(DimEnv)
	if err != nil {
		return sshEnvTarget{}, err
	}
	return sshEnvTarget{client: client, base: adapterBase(org, project) + "/environments/" + url.PathEscape(env)}, nil
}

func sshOnePositional(flags commonFlags, verb, what string) (string, error) {
	if len(flags.positionals) != 1 || flags.positionals[0] == "" {
		return "", failf(ExitUsage, "hikyo %s requires exactly one %s", verb, what)
	}
	return flags.positionals[0], nil
}

// writeFreshFile creates path exclusively; it never overwrites.
func writeFreshFile(path string, content []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return failf(ExitRefused, "writing %s: %v", path, err)
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return failf(ExitRefused, "writing %s: %v", path, err)
	}
	if err := f.Close(); err != nil {
		return failf(ExitRefused, "writing %s: %v", path, err)
	}
	return nil
}

// --- hikyo ssh-ca ---------------------------------------------------------------

func runSSHCA(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("ssh-ca", args, "create", "list", "show", "rotate", "retire-key", "delete", "trusted-keys", "krl")
	if err != nil {
		return err
	}
	var format, algorithm, keyFile, retireKey, overlap, outputFile string
	var stdin bool
	st, flags, err := parseCommon("ssh-ca "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "create" || sub == "rotate" {
			fs.StringVar(&algorithm, "algorithm", "", "key algorithm for a generated key: ed25519, ecdsa-p256 or rsa-3072")
			fs.BoolVar(&stdin, "stdin", false, "import an unencrypted private key read from stdin")
			fs.StringVar(&keyFile, "key-file", "", "import an unencrypted private key read from this file")
		}
		if sub == "rotate" {
			fs.StringVar(&overlap, "overlap", "", "how long the old key stays trusted, e.g. 8h (default: until the old key's last live certificate expires)")
		}
		if sub == "retire-key" {
			fs.StringVar(&retireKey, "key", "", "the retiring key to retire now")
		}
		if sub == "krl" {
			fs.StringVar(&outputFile, "output-file", "", "write the binary KRL to this new file")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	target, err := sshTarget(st, ios, flags)
	if err != nil {
		return err
	}
	base := target.base + "/ssh-cas"
	switch sub {
	case "list":
		if err := flags.checkNoPositionals("ssh-ca list"); err != nil {
			return err
		}
		var out apigen.SSHCAList
		if err := target.client.Do(ctx, http.MethodGet, base, nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, sshCATable(out))
	case "create":
		name, err := sshOnePositional(flags, "ssh-ca create", "CA name")
		if err != nil {
			return err
		}
		private, err := readSSHPrivateKey(ios, stdin, keyFile)
		if err != nil {
			return err
		}
		body := apigen.CreateSSHCARequest{Name: name}
		if algorithm != "" {
			alg := apigen.SSHKeyAlgorithm(algorithm)
			body.Algorithm = &alg
		}
		if private != "" {
			body.PrivateKey = &private
		}
		var out apigen.SSHCA
		if err := target.client.Do(ctx, http.MethodPost, base, body, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, sshCATable(apigen.SSHCAList{Items: []apigen.SSHCA{out}}))
	}
	id, err := sshOnePositional(flags, "ssh-ca "+sub, "CA id")
	if err != nil {
		return err
	}
	path := base + "/" + url.PathEscape(id)
	switch sub {
	case "show":
		var out apigen.SSHCA
		if err := target.client.Do(ctx, http.MethodGet, path, nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, sshCATable(apigen.SSHCAList{Items: []apigen.SSHCA{out}}))
	case "delete":
		if err := target.client.Do(ctx, http.MethodDelete, path, nil, nil); err != nil {
			return err
		}
		fmt.Fprintln(ios.Stderr, "CA deleted. Certificates it signed stay valid on hosts that still trust its keys until they expire: remove its keys from TrustedUserCAKeys.")
		return nil
	case "rotate":
		private, err := readSSHPrivateKey(ios, stdin, keyFile)
		if err != nil {
			return err
		}
		var body apigen.RotateSSHCARequest
		if algorithm != "" {
			alg := apigen.SSHKeyAlgorithm(algorithm)
			body.Algorithm = &alg
		}
		if private != "" {
			body.PrivateKey = &private
		}
		if overlap != "" {
			d, err := time.ParseDuration(overlap)
			if err != nil || d < 0 {
				return failf(ExitUsage, "--overlap must be a non-negative duration, e.g. 8h")
			}
			secs := int64(d / time.Second)
			body.OverlapSeconds = &secs
		}
		var out apigen.SSHCA
		if err := target.client.Do(ctx, http.MethodPost, path+"/rotate", body, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, sshCATable(apigen.SSHCAList{Items: []apigen.SSHCA{out}}))
	case "retire-key":
		if retireKey == "" {
			return failf(ExitUsage, "ssh-ca retire-key requires --key")
		}
		var out apigen.SSHCA
		if err := target.client.Do(ctx, http.MethodPost, path+"/keys/"+url.PathEscape(retireKey)+"/retire", nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, sshCATable(apigen.SSHCAList{Items: []apigen.SSHCA{out}}))
	case "trusted-keys":
		var out string
		if err := target.client.Do(ctx, http.MethodGet, path+"/trusted-keys", nil, &out); err != nil {
			return err
		}
		_, err := io.WriteString(ios.Stdout, out)
		return err
	case "krl":
		if outputFile == "" {
			return failf(ExitUsage, "ssh-ca krl writes binary data and requires --output-file")
		}
		var out []byte
		if err := target.client.Do(ctx, http.MethodGet, path+"/krl", nil, &out); err != nil {
			return err
		}
		return writeFreshFile(outputFile, out, 0o644)
	}
	return failf(ExitUsage, "unknown ssh-ca verb %q", sub)
}

// --- hikyo ssh-profile ----------------------------------------------------------

type sshProfileFlags struct {
	ca, forceCommand, defaultTTL, maxTTL                    string
	principals, sources, extensions, algorithms, requesters stringList
	disabled, noExtensions                                  bool
}

func (p *sshProfileFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&p.ca, "ca", "", "the CA that signs through this profile")
	fs.Var(&p.principals, "principal", "an allowed certificate principal (repeatable)")
	fs.Var(&p.sources, "source-address", "a CIDR certificates are bound to (repeatable)")
	fs.Var(&p.extensions, "extension", "an allowed extension, e.g. permit-pty (repeatable)")
	fs.BoolVar(&p.noExtensions, "no-extensions", false, "allow no extensions")
	fs.Var(&p.algorithms, "key-algorithm", "an allowed user key algorithm (repeatable)")
	fs.Var(&p.requesters, "requester", "a principal id allowed to request certificates (repeatable)")
	fs.StringVar(&p.forceCommand, "force-command", "", "a fixed force-command critical option")
	fs.StringVar(&p.defaultTTL, "default-ttl", "", "default certificate lifetime, e.g. 1h")
	fs.StringVar(&p.maxTTL, "max-ttl", "", "maximum certificate lifetime, e.g. 8h")
	fs.BoolVar(&p.disabled, "disabled", false, "create or leave the profile disabled (issuance refused)")
}

// apply overlays the flags a user actually set onto body.
func (p *sshProfileFlags) apply(fs *flag.FlagSet, body *apigen.SSHProfileRequest) error {
	var err error
	fs.Visit(func(f *flag.Flag) {
		if err != nil {
			return
		}
		switch f.Name {
		case "ca":
			body.CaId = apigen.ID(p.ca)
		case "principal":
			body.Principals = []string(p.principals)
		case "source-address":
			v := []string(p.sources)
			body.SourceAddresses = &v
		case "extension", "no-extensions":
			v := make([]apigen.SSHExtension, 0, len(p.extensions))
			if !p.noExtensions {
				for _, e := range p.extensions {
					v = append(v, apigen.SSHExtension(e))
				}
			}
			body.Extensions = &v
		case "key-algorithm":
			body.KeyAlgorithms = body.KeyAlgorithms[:0]
			for _, a := range p.algorithms {
				body.KeyAlgorithms = append(body.KeyAlgorithms, apigen.SSHKeyAlgorithm(a))
			}
		case "requester":
			v := make([]apigen.ID, 0, len(p.requesters))
			for _, r := range p.requesters {
				v = append(v, apigen.ID(r))
			}
			body.Requesters = &v
		case "force-command":
			v := p.forceCommand
			body.ForceCommand = &v
		case "default-ttl":
			body.DefaultTtlSeconds, err = ttlSeconds(p.defaultTTL)
		case "max-ttl":
			body.MaxTtlSeconds, err = ttlSeconds(p.maxTTL)
		case "disabled":
			enabled := !p.disabled
			body.Enabled = &enabled
		}
	})
	return err
}

func runSSHProfile(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("ssh-profile", args, "create", "update", "list", "show", "delete")
	if err != nil {
		return err
	}
	var format string
	var revokeIssued bool
	var pf sshProfileFlags
	var fs *flag.FlagSet
	st, flags, err := parseCommon("ssh-profile "+sub, ios, rest, func(set *flag.FlagSet) {
		fs = set
		set.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "create" || sub == "update" {
			pf.register(set)
		}
		if sub == "delete" {
			set.BoolVar(&revokeIssued, "revoke-issued", false, "also revoke every live certificate issued through the profile")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	target, err := sshTarget(st, ios, flags)
	if err != nil {
		return err
	}
	base := target.base + "/ssh-profiles"
	switch sub {
	case "list":
		if err := flags.checkNoPositionals("ssh-profile list"); err != nil {
			return err
		}
		var out apigen.SSHProfileList
		if err := target.client.Do(ctx, http.MethodGet, base, nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, sshProfileTable(out))
	case "create":
		name, err := sshOnePositional(flags, "ssh-profile create", "profile name")
		if err != nil {
			return err
		}
		if pf.ca == "" || len(pf.principals) == 0 || len(pf.algorithms) == 0 || pf.defaultTTL == "" || pf.maxTTL == "" {
			return failf(ExitUsage, "ssh-profile create requires --ca, --principal, --key-algorithm, --default-ttl and --max-ttl")
		}
		body := apigen.SSHProfileRequest{Name: name}
		if err := pf.apply(fs, &body); err != nil {
			return err
		}
		var out apigen.SSHProfile
		if err := target.client.Do(ctx, http.MethodPost, base, body, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, sshProfileTable(apigen.SSHProfileList{Items: []apigen.SSHProfile{out}}))
	}
	id, err := sshOnePositional(flags, "ssh-profile "+sub, "profile id")
	if err != nil {
		return err
	}
	path := base + "/" + url.PathEscape(id)
	switch sub {
	case "show":
		var out apigen.SSHProfile
		if err := target.client.Do(ctx, http.MethodGet, path, nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, sshProfileTable(apigen.SSHProfileList{Items: []apigen.SSHProfile{out}}))
	case "update":
		// Read-modify-write: only the flags given change; the PUT replaces
		// the whole profile, so the current state is the base.
		var current apigen.SSHProfile
		if err := target.client.Do(ctx, http.MethodGet, path, nil, &current); err != nil {
			return err
		}
		body := apigen.SSHProfileRequest{
			CaId: current.CaId, Name: current.Name, Principals: current.Principals, ForceCommand: &current.ForceCommand,
			SourceAddresses: &current.SourceAddresses, Extensions: &current.Extensions, KeyAlgorithms: current.KeyAlgorithms,
			DefaultTtlSeconds: current.DefaultTtlSeconds, MaxTtlSeconds: current.MaxTtlSeconds,
			Enabled: &current.Enabled, Requesters: &current.Requesters,
		}
		if err := pf.apply(fs, &body); err != nil {
			return err
		}
		var out apigen.SSHProfile
		if err := target.client.Do(ctx, http.MethodPut, path, body, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, sshProfileTable(apigen.SSHProfileList{Items: []apigen.SSHProfile{out}}))
	case "delete":
		if revokeIssued {
			path += "?revoke_issued=true"
		}
		var out apigen.SSHProfileDeletion
		if err := target.client.Do(ctx, http.MethodDelete, path, nil, &out); err != nil {
			return err
		}
		if revokeIssued {
			fmt.Fprintf(ios.Stderr, "profile deleted; %d certificates revoked.\n", out.RevokedCertificateCount)
		} else {
			fmt.Fprintln(ios.Stderr, "profile deleted; certificates it issued stay valid until they expire (use --revoke-issued to revoke them).")
		}
		return nil
	}
	return failf(ExitUsage, "unknown ssh-profile verb %q", sub)
}

// --- hikyo ssh-cert ------------------------------------------------------------

func runSSHCert(ctx context.Context, ios IO, args []string) (returnErr error) {
	sub, rest, err := subverb("ssh-cert", args, "issue", "list", "show", "revoke")
	if err != nil {
		return err
	}
	var format, profile, publicKeyFile, algorithm, ttl, outputFile, certFile string
	var principals, sources, extensions stringList
	var noExtensions, dangerous bool
	st, flags, err := parseCommon("ssh-cert "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "issue" {
			fs.StringVar(&profile, "profile", "", "the profile to issue through")
			fs.StringVar(&publicKeyFile, "public-key-file", "", "certify this public key (e.g. ~/.ssh/id_ed25519.pub) instead of generating one")
			fs.StringVar(&algorithm, "key-algorithm", "", "algorithm of a generated key (default ed25519)")
			fs.Var(&principals, "principal", "a principal to include (repeatable)")
			fs.Var(&sources, "source-address", "narrow the source addresses (repeatable CIDR)")
			fs.Var(&extensions, "extension", "an extension to include (repeatable)")
			fs.BoolVar(&noExtensions, "no-extensions", false, "include no extensions")
			fs.StringVar(&ttl, "ttl", "", "certificate lifetime, e.g. 1h (default: the profile's)")
			fs.StringVar(&certFile, "cert-file", "", "write the certificate to this new file (default: stdout, or <output-file>-cert.pub)")
			fs.StringVar(&outputFile, "output-file", "", "write a generated private key to a fresh 0600 file")
			fs.BoolVar(&dangerous, "dangerously-print", false, "write a generated private key to stdout (and whatever collects it)")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	var sink *disclose.PreparedSink
	var publicKey string
	if sub == "issue" {
		if publicKeyFile != "" {
			if outputFile != "" || dangerous || algorithm != "" {
				return failf(ExitUsage, "--public-key-file certifies your key; --output-file, --dangerously-print and --key-algorithm apply only to a generated key")
			}
			raw, err := os.ReadFile(publicKeyFile)
			if err != nil {
				return failf(ExitRefused, "reading the public key: %v", err)
			}
			publicKey = strings.TrimSpace(string(raw))
		} else {
			// Prepare the display-once sink BEFORE any network call.
			sink, err = ios.prepareDisclosure(disclose.Options{OutputFile: outputFile, DangerouslyPrint: dangerous, Stdout: ios.Stdout})
			if err != nil {
				return failf(ExitRefused, "the private key has nowhere to go: %v", err)
			}
			defer sink.AbortOnReturn(&returnErr)
			if certFile == "" && outputFile != "" {
				certFile = outputFile + "-cert.pub"
			}
		}
	}
	target, err := sshTarget(st, ios, flags)
	if err != nil {
		return err
	}
	base := target.base + "/ssh-certificates"
	switch sub {
	case "list":
		if err := flags.checkNoPositionals("ssh-cert list"); err != nil {
			return err
		}
		var out apigen.SSHCertificateList
		if err := target.client.Do(ctx, http.MethodGet, base, nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, sshCertificateTable(out))
	case "show", "revoke":
		id, err := sshOnePositional(flags, "ssh-cert "+sub, "certificate id")
		if err != nil {
			return err
		}
		method, path := http.MethodGet, base+"/"+url.PathEscape(id)
		if sub == "revoke" {
			method, path = http.MethodPost, path+"/revoke"
		}
		var out apigen.SSHCertificate
		if err := target.client.Do(ctx, method, path, nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, sshCertificateTable(apigen.SSHCertificateList{Items: []apigen.SSHCertificate{out}}))
	case "issue":
		if err := flags.checkNoPositionals("ssh-cert issue"); err != nil {
			return err
		}
		if profile == "" {
			return failf(ExitUsage, "ssh-cert issue requires --profile")
		}
		body := apigen.IssueSSHCertificateRequest{ProfileId: apigen.ID(profile)}
		if publicKey != "" {
			body.PublicKey = &publicKey
		}
		if algorithm != "" {
			alg := apigen.SSHKeyAlgorithm(algorithm)
			body.KeyAlgorithm = &alg
		}
		if len(principals) > 0 {
			v := []string(principals)
			body.Principals = &v
		}
		if len(sources) > 0 {
			v := []string(sources)
			body.SourceAddresses = &v
		}
		if len(extensions) > 0 || noExtensions {
			v := make([]apigen.SSHExtension, 0, len(extensions))
			for _, e := range extensions {
				v = append(v, apigen.SSHExtension(e))
			}
			body.Extensions = &v
		}
		if ttl != "" {
			secs, err := ttlSeconds(ttl)
			if err != nil {
				return err
			}
			body.TtlSeconds = &secs
		}
		var out apigen.SSHCertificateIssue
		if err := target.client.Do(ctx, http.MethodPost, base, body, &out); err != nil {
			return err
		}
		if sink != nil {
			if out.PrivateKey == nil {
				return failf(ExitInternal, "the server issued a generated-key certificate without its private key")
			}
			if _, err := sink.WriteOnce("hikyo SSH private key (shown once)", *out.PrivateKey); err != nil {
				return failf(ExitRefused, "disclosing the private key: %v", err)
			}
		}
		// The private key went only to the sink; it is never rendered.
		out.PrivateKey = nil
		if certFile != "" {
			if err := writeFreshFile(certFile, []byte(out.CertificateText+"\n"), 0o644); err != nil {
				return err
			}
		}
		if f == FormatJSON {
			return Render(ios.Stdout, f, Table{JSON: out})
		}
		if certFile == "" {
			if _, err := fmt.Fprintln(ios.Stdout, out.CertificateText); err != nil {
				return err
			}
		}
		// The summary goes to stderr so stdout stays the bare certificate.
		return Render(ios.Stderr, f, sshCertificateTable(apigen.SSHCertificateList{Items: []apigen.SSHCertificate{out.Certificate}}))
	}
	return failf(ExitUsage, "unknown ssh-cert verb %q", sub)
}
