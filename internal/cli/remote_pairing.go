package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
	"github.com/Hikyo-Org/hikyo/internal/pairing"
)

const pairingRequestsPath = api.PathPrefix + "/instance/pairing-requests"
const pairingInvitesPath = api.PathPrefix + "/instance/pairing-invites"
const pairingStateLimit = 65536
const pairingRequestFileLimit = 131072

// Preparation addresses only the selected local instance. No remote address is
// accepted until its administrator has manually created an invitation.
func runRemotePairing(ctx context.Context, ios IO, args []string) (returnErr error) {
	sub, rest, err := subverb("remote-pairing", args, "prepare", "accept")
	if err != nil {
		return err
	}
	var outputFile, requestFile, inviteFile, format string
	var dangerous, customSecret bool
	var words int
	var pairingFlags *flag.FlagSet
	st, flags, err := parseCommon("remote-pairing "+sub, ios, rest, func(fs *flag.FlagSet) {
		pairingFlags = fs
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "prepare" {
			fs.StringVar(&outputFile, "output-file", "", "create a private file containing the phrase and local continuation")
			fs.BoolVar(&dangerous, "dangerously-print", false, "disclose the phrase and continuation to stdout")
			fs.BoolVar(&customSecret, "custom-secret", false, "prompt privately for your own password or phrase")
			fs.IntVar(&words, "words", pairing.PhraseWords, "number of random words to generate, 1 to 128")
		} else {
			fs.StringVar(&requestFile, "request-file", "", "private file saved by remote-pairing prepare; otherwise prompt privately")
			fs.StringVar(&inviteFile, "invite-file", "", "private invitation file; otherwise prompt privately")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	if sub == "prepare" {
		if err := flags.checkNoPositionals("remote-pairing prepare"); err != nil {
			return err
		}
		wordsSpecified := false
		pairingFlags.Visit(func(value *flag.Flag) { wordsSpecified = wordsSpecified || value.Name == "words" })
		if words < 1 || words > pairing.MaxPhraseWords || customSecret && wordsSpecified {
			return failf(ExitUsage, "choose --custom-secret or --words 1 to 128")
		}
		sink, err := ios.prepareDisclosure(disclose.Options{OutputFile: outputFile, DangerouslyPrint: dangerous, Stdout: ios.Stdout})
		if err != nil {
			return failf(ExitRefused, "the pairing phrase has nowhere to go: %v", err)
		}
		defer sink.AbortOnReturn(&returnErr)
		body := apigen.PrepareRemotePairingRequest{}
		if customSecret {
			secret, err := ios.readPassword("Your pairing password or phrase (case and spaces are significant): ")
			if err != nil {
				return failf(ExitRefused, "reading the pairing secret")
			}
			if pairing.ValidatePhrase(secret) != nil {
				return failf(ExitUsage, "the pairing secret must contain 1 to 4096 UTF-8 bytes")
			}
			body.Phrase = &secret
		} else {
			body.WordCount = &words
		}
		client, _, _, err := authenticatedTarget(st, ios, flags)
		if err != nil {
			return err
		}
		var out apigen.RemotePairingPrepared
		if err := client.Do(ctx, http.MethodPost, pairingRequestsPath, body, &out); err != nil {
			return err
		}
		raw, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return failf(ExitInternal, "encoding the pairing continuation")
		}
		if _, err := sink.WriteOnce("Hikyo pairing phrase and local continuation", string(raw)); err != nil {
			return failf(ExitRefused, "disclosing the pairing continuation: %v", err)
		}
		fmt.Fprintln(ios.Stderr, "Share only the phrase with the remote administrator. Keep requester_state private for accepting the invitation on this instance.")
		return Render(ios.Stdout, f, Table{Columns: []string{"EXPIRES"}, Rows: [][]string{{out.ExpiresAt.Format(time.RFC3339)}}, JSON: struct {
			ExpiresAt time.Time `json:"expires_at"`
		}{out.ExpiresAt}})
	}
	if len(flags.positionals) != 1 {
		return failf(ExitUsage, "usage: hikyo remote-pairing accept <name> [--request-file PATH] [--invite-file PATH]")
	}
	state, err := readPairingContinuation(ios, requestFile)
	if err != nil {
		return err
	}
	invite, err := readPairingInput(ios, inviteFile, "Paste the remote administrator's invitation: ", 8192)
	if err != nil {
		return err
	}
	client, _, _, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return err
	}
	var out apigen.Remote
	if err := client.Do(ctx, http.MethodPost, remotesPath+"/pair", apigen.PairRemoteRequest{
		Name: flags.positionals[0], RequesterState: state, InviteBundle: invite,
	}, &out); err != nil {
		return err
	}
	return Render(ios.Stdout, f, remoteTable(apigen.RemoteList{Items: []apigen.Remote{out}, Count: 1}))
}

func runRemoteInvite(ctx context.Context, ios IO, args []string) (returnErr error) {
	sub, rest, err := subverb("remote-invite", args, "create", "list", "revoke")
	if err != nil {
		return err
	}
	var label, id, outputFile, format string
	var lifetime, credentialLifetime time.Duration
	var dangerous bool
	st, flags, err := parseCommon("remote-invite "+sub, ios, rest, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		if sub == "create" {
			fs.StringVar(&label, "label", "", "names the intended peer")
			fs.DurationVar(&lifetime, "lifetime", 10*time.Minute, "invitation validity, at most 1h")
			fs.DurationVar(&credentialLifetime, "credential-lifetime", 30*24*time.Hour, "finite connection credential lifetime")
			fs.StringVar(&outputFile, "output-file", "", "create a private file containing the invitation")
			fs.BoolVar(&dangerous, "dangerously-print", false, "disclose the invitation to stdout")
		}
		if sub == "revoke" {
			fs.StringVar(&id, "id", "", "the invitation to revoke")
		}
	})
	if err != nil {
		return err
	}
	f, err := ParseFormat(format)
	if err != nil {
		return err
	}
	if err := flags.checkNoPositionals("remote-invite " + sub); err != nil {
		return err
	}
	if sub == "revoke" && id == "" {
		return failf(ExitUsage, "usage: hikyo remote-invite revoke --id <invitation-id>")
	}
	if sub != "create" {
		client, _, _, err := authenticatedTarget(st, ios, flags)
		if err != nil {
			return err
		}
		if sub == "revoke" {
			return client.Do(ctx, http.MethodDelete, pairingInvitesPath+"/"+url.PathEscape(id), nil, nil)
		}
		var out apigen.RemotePairingInviteList
		if err := client.Do(ctx, http.MethodGet, pairingInvitesPath, nil, &out); err != nil {
			return err
		}
		return Render(ios.Stdout, f, pairingInviteTable(out, ios.now()))
	}
	if strings.TrimSpace(label) == "" {
		return failf(ExitUsage, "usage: hikyo remote-invite create --label <peer>")
	}
	if lifetime < time.Second || lifetime > time.Hour || credentialLifetime <= lifetime || credentialLifetime > 365*24*time.Hour {
		return failf(ExitUsage, "invitation lifetime must be 1s to 1h; connection lifetime must exceed invitation lifetime and be at most 8760h")
	}
	sink, err := ios.prepareDisclosure(disclose.Options{OutputFile: outputFile, DangerouslyPrint: dangerous, Stdout: ios.Stdout})
	if err != nil {
		return failf(ExitRefused, "the invitation has nowhere to go: %v", err)
	}
	defer sink.AbortOnReturn(&returnErr)
	phrase, err := ios.readPassword("Pairing phrase received from the requester: ")
	if err != nil {
		return failf(ExitRefused, "reading the pairing phrase: %v", err)
	}
	client, _, _, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return err
	}
	var out apigen.RemotePairingInviteCreated
	if err := client.Do(ctx, http.MethodPost, pairingInvitesPath, apigen.CreateRemotePairingInviteRequest{
		Phrase: phrase, Label: label,
		InviteLifetimeSeconds: int(lifetime / time.Second), CredentialLifetimeSeconds: int(credentialLifetime / time.Second),
	}, &out); err != nil {
		return err
	}
	if _, err := sink.WriteOnce("Hikyo single-use pairing invitation", out.InviteBundle); err != nil {
		return failf(ExitRefused, "disclosing the invitation: %v", err)
	}
	return Render(ios.Stdout, f, pairingInviteTable(apigen.RemotePairingInviteList{Items: []apigen.RemotePairingInvite{out.Invite}, Count: 1}, ios.now()))
}

func pairingInviteTable(list apigen.RemotePairingInviteList, now time.Time) Table {
	rows := make([][]string, 0, len(list.Items))
	for _, invite := range list.Items {
		state := "pending"
		switch {
		case invite.RevokedAt != nil:
			state = "revoked"
		case invite.ConsumedAt != nil:
			state = "consumed"
		case !now.Before(invite.ExpiresAt):
			state = "expired"
		}
		rows = append(rows, []string{invite.Id, invite.Label, invite.Origin, state, invite.ExpiresAt.Format(time.RFC3339)})
	}
	return Table{Columns: []string{"ID", "LABEL", "ORIGIN", "STATE", "EXPIRES"}, Rows: rows, JSON: list}
}

func readPairingContinuation(ios IO, path string) (string, error) {
	if path == "" {
		return readPairingInput(ios, "", "Paste the private requester_state saved during preparation: ", pairingStateLimit)
	}
	raw, err := readPairingInput(ios, path, "", pairingRequestFileLimit)
	if err != nil {
		return "", err
	}
	var prepared apigen.RemotePairingPrepared
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&prepared); err != nil {
		return "", failf(ExitUsage, "the request file must contain the JSON saved by remote-pairing prepare")
	}
	if decoder.Decode(new(json.RawMessage)) != io.EOF || len(prepared.RequesterState) == 0 || len(prepared.RequesterState) > pairingStateLimit {
		return "", failf(ExitUsage, "the request file contains an invalid pairing continuation")
	}
	if !ios.now().Before(prepared.ExpiresAt) {
		return "", failf(ExitRefused, "the pairing continuation expired; prepare a new phrase")
	}
	return prepared.RequesterState, nil
}

// Sensitive imports use platform-specific ownership and access checks before
// reading a bounded payload from the verified handle.
func readPairingInput(ios IO, path, prompt string, limit int) (string, error) {
	if path == "" {
		value, err := ios.readPassword(prompt)
		if err != nil {
			return "", failf(ExitRefused, "reading pairing input: %v", err)
		}
		value = strings.TrimSpace(value)
		if len(value) == 0 || len(value) > limit {
			return "", failf(ExitUsage, "pairing input must contain 1 to %d bytes", limit)
		}
		return value, nil
	}
	file, err := openPairingInputFile(path)
	if err != nil {
		return "", failf(ExitRefused, "pairing input must be a private regular file owned by you")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(raw) == 0 || len(raw) > limit {
		return "", failf(ExitUsage, "pairing input must contain 1 to %d bytes", limit)
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", failf(ExitUsage, "pairing input cannot be blank")
	}
	return string(raw), nil
}
