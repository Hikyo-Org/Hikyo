package cli

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
)

// Transit verbs (#156, transit ADR D10). Inputs (plaintext, messages,
// ciphertexts, associated data) are read from stdin or a file, never argv:
// process arguments are public. Plaintext outputs (decrypt, a revealed data key)
// leave through the display-once print triad, exactly like a leased credential.

func transitKeyTable(list apigen.TransitKeyList) Table {
	rows := make([][]string, 0, len(list.Items))
	for _, k := range list.Items {
		state := string(k.State)
		if k.CompromisedThroughVersion > 0 {
			state += fmt.Sprintf(" (compromised<=v%d)", k.CompromisedThroughVersion)
		}
		if k.RotationDue {
			state += " (rotation due)"
		}
		ops := make([]string, 0, len(k.AllowedOperations))
		for _, op := range k.AllowedOperations {
			ops = append(ops, string(op))
		}
		rows = append(rows, []string{
			k.Name, string(k.Algorithm), string(k.Custody), state,
			fmt.Sprintf("v%d", k.LatestVersion), fmt.Sprintf("v%d/v%d", k.MinEncryptVersion, k.MinDecryptVersion),
			strings.Join(ops, ","),
		})
	}
	return Table{
		// No material column: key material never leaves custody.
		Columns: []string{"NAME", "ALGORITHM", "CUSTODY", "STATE", "LATEST", "MIN ENC/DEC", "OPERATIONS"},
		Rows:    rows,
		JSON:    list,
	}
}

// transitInput is the one input path for data-plane verbs: exactly one of
// --stdin or --input-file, bounded before it is sent.
type transitInput struct {
	stdin bool
	file  string
}

func (s transitInput) read(ios IO, what string, bound int) ([]byte, error) {
	if s.stdin == (s.file != "") {
		return nil, failf(ExitUsage, "read the %s from exactly one of --stdin or --input-file", what)
	}
	var r io.Reader
	if s.stdin {
		r = ios.Stdin
	} else {
		f, err := os.Open(s.file)
		if err != nil {
			return nil, failf(ExitUsage, "open --input-file: %v", err)
		}
		defer f.Close()
		r = f
	}
	raw, err := io.ReadAll(io.LimitReader(r, int64(bound)+1))
	if err != nil {
		return nil, failf(ExitUsage, "read the %s: %v", what, err)
	}
	if len(raw) > bound {
		return nil, failf(ExitUsage, "the %s exceeds %d bytes", what, bound)
	}
	return raw, nil
}

// readAAD reads optional associated data (the API's `context`) from a file.
func readAAD(path string) (*string, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, failf(ExitUsage, "read --aad-file: %v", err)
	}
	if len(raw) > crypto.MaxTransitContextBytes {
		return nil, failf(ExitUsage, "--aad-file exceeds %d bytes", crypto.MaxTransitContextBytes)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	return &encoded, nil
}

// parseTransitCallers parses repeated --caller principal=op,op values.
type callerFlags []apigen.TransitCaller

func (c *callerFlags) String() string { return "" }

func (c *callerFlags) Set(v string) error {
	principal, ops, ok := strings.Cut(v, "=")
	if !ok || principal == "" || ops == "" {
		return fmt.Errorf("--caller takes principal=operation[,operation...]")
	}
	entry := apigen.TransitCaller{PrincipalId: principal}
	for _, op := range strings.Split(ops, ",") {
		entry.Operations = append(entry.Operations, apigen.TransitOperation(strings.TrimSpace(op)))
	}
	*c = append(*c, entry)
	return nil
}

func runTransit(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("transit", args, "key", "encrypt", "decrypt", "rewrap", "datakey", "sign", "verify", "hmac", "hmac-verify")
	if err != nil {
		return err
	}
	if sub == "key" {
		return runTransitKey(ctx, ios, rest)
	}
	return runTransitData(ctx, ios, sub, rest)
}

func transitBase(ios IO, st *State, flags commonFlags) (*Client, string, error) {
	client, _, resolved, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return nil, "", err
	}
	org, err := resolved.Require(DimOrg)
	if err != nil {
		return nil, "", err
	}
	project, err := resolved.Require(DimProject)
	if err != nil {
		return nil, "", err
	}
	env, err := resolved.Require(DimEnv)
	if err != nil {
		return nil, "", err
	}
	return client, adapterBase(org, project) + "/environments/" + url.PathEscape(env) + "/transit-keys", nil
}

var transitLifecycleVerbs = map[string]bool{
	"disable": true, "enable": true, "retire": true, "compromise": true,
	"schedule-deletion": true, "cancel-deletion": true,
}

func runTransitKey(ctx context.Context, ios IO, args []string) error {
	sub, rest, err := subverb("transit key", args, "create", "list", "show", "configure", "rotate", "trim",
		"disable", "enable", "retire", "compromise", "schedule-deletion", "cancel-deletion")
	if err != nil {
		return err
	}
	var format, algorithm, custody, allow, rotation, delay string
	var minEnc, minDec int64
	var clearCallers bool
	var callers callerFlags
	st, flags, err := parseCommon("transit key "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		switch sub {
		case "create":
			fs.StringVar(&algorithm, "algorithm", "", "xchacha20-poly1305, ed25519 or hmac-sha256")
			fs.StringVar(&custody, "custody", "", "software (default) or external")
			fs.StringVar(&allow, "allow", "", "comma-separated allowed operations (default: all but datakey-plaintext)")
			fs.StringVar(&rotation, "rotation-period", "", "automatic rotation period, e.g. 720h (at least 1h)")
			fs.Var(&callers, "caller", "restrict use to principal=operation[,operation...] (repeatable)")
		case "configure":
			fs.Int64Var(&minEnc, "min-encrypt-version", 0, "lowest version new output may use")
			fs.Int64Var(&minDec, "min-decrypt-version", 0, "lowest version presented input may use")
			fs.StringVar(&rotation, "rotation-period", "", "automatic rotation period, or 0 to disable")
			fs.Var(&callers, "caller", "replace the caller entries with principal=operation[,operation...] (repeatable)")
			fs.BoolVar(&clearCallers, "clear-callers", false, "remove every caller entry")
		case "schedule-deletion":
			fs.StringVar(&delay, "delay", "", "deletion delay, between 24h and 2160h (default 168h)")
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
		if err := flags.checkNoPositionals("transit key list"); err != nil {
			return err
		}
	} else if len(flags.positionals) != 1 || name == "" {
		return failf(ExitUsage, "usage: hikyo transit key %s takes exactly one key name, got %d", sub, len(flags.positionals))
	}
	client, base, err := transitBase(ios, st, flags)
	if err != nil {
		return err
	}
	keyPath := base + "/" + url.PathEscape(name)
	render := func(k apigen.TransitKey) error {
		return Render(ios.Stdout, f, transitKeyTable(apigen.TransitKeyList{Items: []apigen.TransitKey{k}}))
	}
	var out apigen.TransitKey
	switch {
	case sub == "list":
		var list apigen.TransitKeyList
		if err := client.Do(ctx, http.MethodGet, base, nil, &list); err != nil {
			return err
		}
		return Render(ios.Stdout, f, transitKeyTable(list))
	case sub == "show":
		if err := client.Do(ctx, http.MethodGet, keyPath, nil, &out); err != nil {
			return err
		}
		return render(out)
	case sub == "create":
		if algorithm == "" {
			return failf(ExitUsage, "transit key create requires --algorithm")
		}
		body := apigen.CreateTransitKeyRequest{Name: name, Algorithm: apigen.TransitAlgorithm(algorithm)}
		if custody != "" {
			c := apigen.TransitCustody(custody)
			body.Custody = &c
		}
		if allow != "" {
			ops := []apigen.TransitOperation{}
			for _, op := range strings.Split(allow, ",") {
				ops = append(ops, apigen.TransitOperation(strings.TrimSpace(op)))
			}
			body.AllowedOperations = &ops
		}
		if rotation != "" {
			secs, err := transitSeconds("--rotation-period", rotation)
			if err != nil {
				return err
			}
			body.RotationPeriodSeconds = &secs
		}
		if len(callers) > 0 {
			c := []apigen.TransitCaller(callers)
			body.Callers = &c
		}
		if err := client.Do(ctx, http.MethodPost, base, body, &out); err != nil {
			return err
		}
		return render(out)
	case sub == "configure":
		var body apigen.ConfigureTransitKeyRequest
		if minEnc > 0 {
			body.MinEncryptVersion = &minEnc
		}
		if minDec > 0 {
			body.MinDecryptVersion = &minDec
		}
		if rotation != "" {
			secs, err := transitSeconds("--rotation-period", rotation)
			if err != nil {
				return err
			}
			body.RotationPeriodSeconds = &secs
		}
		if clearCallers && len(callers) > 0 {
			return failf(ExitUsage, "--clear-callers and --caller are mutually exclusive")
		}
		if clearCallers || len(callers) > 0 {
			c := []apigen.TransitCaller(callers)
			if c == nil {
				c = []apigen.TransitCaller{}
			}
			body.Callers = &c
		}
		if err := client.Do(ctx, http.MethodPatch, keyPath, body, &out); err != nil {
			return err
		}
		return render(out)
	case sub == "rotate":
		if err := client.Do(ctx, http.MethodPost, keyPath+"/rotate", nil, &out); err != nil {
			return err
		}
		return render(out)
	case sub == "trim":
		var trimmed apigen.TransitTrimResult
		if err := client.Do(ctx, http.MethodPost, keyPath+"/trim", nil, &trimmed); err != nil {
			return err
		}
		fmt.Fprintf(ios.Stderr, "%d version(s) permanently deleted\n", trimmed.VersionsDeleted)
		return render(trimmed.Key)
	case transitLifecycleVerbs[sub]:
		body := apigen.TransitLifecycleRequest{Action: apigen.TransitLifecycleRequestAction(sub)}
		if delay != "" {
			secs, err := transitSeconds("--delay", delay)
			if err != nil {
				return err
			}
			body.DelaySeconds = &secs
		}
		if err := client.Do(ctx, http.MethodPost, keyPath+"/lifecycle", body, &out); err != nil {
			return err
		}
		return render(out)
	}
	return failf(ExitUsage, "unknown transit key verb %q", sub)
}

// transitSeconds parses a duration flag into whole seconds; "0" disables.
func transitSeconds(flagName, raw string) (int64, error) {
	if raw == "0" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < time.Second {
		return 0, failf(ExitUsage, "%s must be a duration such as 720h", flagName)
	}
	return int64(d / time.Second), nil
}

func runTransitData(ctx context.Context, ios IO, sub string, args []string) (returnErr error) {
	var input transitInput
	var aadFile, signature, mac, outputFile string
	var keyVersion int64
	var bits int
	var reveal, dangerous bool
	st, flags, err := parseCommon("transit "+sub, ios, args, func(fs *flag.FlagSet) {
		fs.StringVar(&aadFile, "aad-file", "", "associated data (the API's context) read from this file")
		switch sub {
		case "datakey":
			fs.IntVar(&bits, "bits", 256, "data key size: 128, 256 or 512")
			fs.BoolVar(&reveal, "plaintext", false, "also disclose the plaintext data key (display-once)")
		default:
			fs.BoolVar(&input.stdin, "stdin", false, "read the input from stdin")
			fs.StringVar(&input.file, "input-file", "", "read the input from this file")
		}
		switch sub {
		case "encrypt", "sign", "hmac":
			fs.Int64Var(&keyVersion, "key-version", 0, "produce under this version instead of the latest")
		case "verify":
			fs.StringVar(&signature, "signature", "", "the signature to check (hikyo:vN:...)")
		case "hmac-verify":
			fs.StringVar(&mac, "mac", "", "the MAC to check (hikyo:vN:...)")
		}
		if sub == "decrypt" || sub == "datakey" {
			fs.StringVar(&outputFile, "output-file", "", "write the plaintext to a fresh 0600 file")
			fs.BoolVar(&dangerous, "dangerously-print", false, "write the plaintext to stdout (and whatever collects it)")
		}
	})
	if err != nil {
		return err
	}
	name := flags.positional()
	if len(flags.positionals) != 1 || name == "" {
		return failf(ExitUsage, "usage: hikyo transit %s takes exactly one key name, got %d", sub, len(flags.positionals))
	}
	var sink *disclose.PreparedSink
	if sub == "decrypt" || (sub == "datakey" && reveal) {
		// Prepare the display-once sink BEFORE the call, so the reserved
		// destination is the exact destination the plaintext reaches.
		sink, err = ios.prepareDisclosure(disclose.Options{OutputFile: outputFile, DangerouslyPrint: dangerous, Stdout: ios.Stdout})
		if err != nil {
			return failf(ExitRefused, "the plaintext has nowhere to go: %v", err)
		}
		defer sink.AbortOnReturn(&returnErr)
	}
	aad, err := readAAD(aadFile)
	if err != nil {
		return err
	}
	var version *int64
	if keyVersion > 0 {
		version = &keyVersion
	}
	client, base, err := transitBase(ios, st, flags)
	if err != nil {
		return err
	}
	path := base + "/" + url.PathEscape(name) + "/" + sub
	readB64 := func(what string) (string, error) {
		raw, err := input.read(ios, what, crypto.MaxTransitPlaintextBytes)
		if err != nil {
			return "", err
		}
		defer crypto.Zero(raw)
		return base64.StdEncoding.EncodeToString(raw), nil
	}
	readText := func(what string) (string, error) {
		raw, err := input.read(ios, what, crypto.MaxTransitWireBytes)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(raw)), nil
	}
	switch sub {
	case "encrypt":
		plaintext, err := readB64("plaintext")
		if err != nil {
			return err
		}
		var out apigen.TransitCiphertextResult
		if err := client.Do(ctx, http.MethodPost, path, apigen.TransitEncryptRequest{Plaintext: plaintext, Context: aad, KeyVersion: version}, &out); err != nil {
			return err
		}
		fmt.Fprintln(ios.Stdout, out.Ciphertext)
		return nil
	case "decrypt":
		ciphertext, err := readText("ciphertext")
		if err != nil {
			return err
		}
		var out apigen.TransitDecryptResult
		if err := client.Do(ctx, http.MethodPost, path, apigen.TransitDecryptRequest{Ciphertext: ciphertext, Context: aad}, &out); err != nil {
			return err
		}
		plaintext, err := base64.StdEncoding.DecodeString(out.Plaintext)
		if err != nil {
			return failf(ExitInternal, "the server's plaintext did not match the contract")
		}
		defer crypto.Zero(plaintext)
		if _, err := sink.WriteOnce("hikyo transit plaintext (shown once)", string(plaintext)); err != nil {
			return failf(ExitRefused, "disclosing the plaintext: %v", err)
		}
		return nil
	case "rewrap":
		ciphertext, err := readText("ciphertext")
		if err != nil {
			return err
		}
		var out apigen.TransitCiphertextResult
		if err := client.Do(ctx, http.MethodPost, path, apigen.TransitDecryptRequest{Ciphertext: ciphertext, Context: aad}, &out); err != nil {
			return err
		}
		fmt.Fprintln(ios.Stdout, out.Ciphertext)
		return nil
	case "datakey":
		b := apigen.TransitDataKeyRequestBits(bits)
		body := apigen.TransitDataKeyRequest{Bits: &b, Context: aad}
		if reveal {
			body.Plaintext = &reveal
		}
		var out apigen.TransitDataKeyResult
		if err := client.Do(ctx, http.MethodPost, path, body, &out); err != nil {
			return err
		}
		if reveal {
			if out.Plaintext == nil {
				return failf(ExitInternal, "the server returned no plaintext data key")
			}
			if _, err := sink.WriteOnce("hikyo transit data key, base64 (shown once)", *out.Plaintext); err != nil {
				return failf(ExitRefused, "disclosing the data key: %v", err)
			}
		}
		fmt.Fprintln(ios.Stdout, out.Ciphertext)
		return nil
	case "sign":
		message, err := readB64("message")
		if err != nil {
			return err
		}
		var out apigen.TransitSignatureResult
		if err := client.Do(ctx, http.MethodPost, path, apigen.TransitSignRequest{Message: message, KeyVersion: version}, &out); err != nil {
			return err
		}
		fmt.Fprintln(ios.Stdout, out.Signature)
		return nil
	case "hmac":
		message, err := readB64("message")
		if err != nil {
			return err
		}
		var out apigen.TransitHMACResult
		if err := client.Do(ctx, http.MethodPost, path, apigen.TransitHMACRequest{Message: message, KeyVersion: version}, &out); err != nil {
			return err
		}
		fmt.Fprintln(ios.Stdout, out.Mac)
		return nil
	case "verify", "hmac-verify":
		presented := signature
		if sub == "hmac-verify" {
			presented = mac
		}
		if presented == "" {
			return failf(ExitUsage, "transit %s requires --%s", sub, map[string]string{"verify": "signature", "hmac-verify": "mac"}[sub])
		}
		message, err := readB64("message")
		if err != nil {
			return err
		}
		var body any = apigen.TransitVerifyRequest{Message: message, Signature: presented}
		if sub == "hmac-verify" {
			body = apigen.TransitHMACVerifyRequest{Message: message, Mac: presented}
		}
		var out apigen.TransitVerifyResult
		if err := client.Do(ctx, http.MethodPost, path, body, &out); err != nil {
			return err
		}
		if !out.Valid {
			fmt.Fprintln(ios.Stdout, "invalid")
			return failf(ExitRefused, "the %s does not verify under key version %s", map[string]string{"verify": "signature", "hmac-verify": "MAC"}[sub], strconv.FormatInt(out.KeyVersion, 10))
		}
		fmt.Fprintln(ios.Stdout, "valid")
		return nil
	}
	return failf(ExitUsage, "unknown transit verb %q", sub)
}
