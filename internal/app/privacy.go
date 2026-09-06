package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// runAdminPrivacy exposes no network route. Root-key boot and the same strict
// owner-only file sink as credential recovery apply to exports and receipts.
func runAdminPrivacy(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string, stderr io.Writer) (returnErr error) {
	if len(args) == 0 {
		return errors.New("usage: hikyo admin privacy export|restrict|erase|release|correct|reapply")
	}
	action := args[0]
	switch action {
	case "export", "restrict", "erase", "release", "correct", "reapply":
	default:
		return errors.New("privacy: unknown action")
	}
	fs := flag.NewFlagSet("admin privacy "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	username := fs.String("username", "", "corrected local login handle (correct only)")
	displayName := fs.String("display-name", "", "corrected local display name (correct only)")
	principal := fs.String("principal", "", "explicit human principal ID")
	output := fs.String("output-file", "", "new owner-only export or receipt file; retained separately from backups")
	receiptPath := fs.String("receipt", "", "owner-only receipt to reapply after restore")
	confirm := fs.Bool("confirm", false, "confirm the named subject action; erase permanently removes identity and credentials")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if action != "correct" && (*username != "" || *displayName != "") {
		return errors.New("privacy: identity labels require correct action")
	}
	if fs.NArg() != 0 || *output == "" {
		return errors.New("privacy: --output-file is required; positional arguments are refused")
	}
	if action != "export" && !*confirm {
		return errors.New("privacy: review the target and add --confirm; erase permanently removes identity and credentials")
	}
	if (action == "reapply" && (*receiptPath == "" || *principal != "")) || (action != "reapply" && (*principal == "" || *receiptPath != "")) {
		return errors.New("privacy: provide --principal, or --receipt only for reapply")
	}
	var receipt service.PrivacyReceipt
	if action == "reapply" {
		raw, err := readPrivacyReceipt(*receiptPath)
		if err != nil {
			return err
		}
		receipt, err = parsePrivacyReceipt(raw)
		if err != nil {
			return err
		}
	}
	sink, err := disclose.Prepare(disclose.Options{OutputFile: *output}, nil)
	if err != nil {
		return err
	}
	defer sink.AbortOnReturn(&returnErr)
	auth, closeDB, err := adminAuth(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer closeDB()
	var result any
	if action == "correct" {
		result, err = auth.CorrectPrivacySubject(ctx, *principal, *username, *displayName)
	} else if action == "export" {
		result, err = auth.ExportPrivacySubject(ctx, *principal)
	} else if action == "reapply" {
		result, err = auth.ReapplyPrivacyReceipt(ctx, receipt)
	} else {
		result, err = auth.ApplyPrivacySubject(ctx, *principal, action, "")
	}
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if _, err := sink.WriteOnce("Privacy "+action, string(raw)); err != nil {
		return fmt.Errorf("privacy %s committed, but file delivery failed; repeat the same action to obtain a receipt: %w", action, err)
	}
	fmt.Fprintf(stderr, "privacy %s complete; owner-only file written. Retain restriction/erasure receipts separately from backups; retire superseded restrictions after release. Review documented remaining data.\n", action)
	return nil
}

func parsePrivacyReceipt(raw []byte) (service.PrivacyReceipt, error) {
	var receipt service.PrivacyReceipt
	// Reject duplicate keys before decoding the typed, closed schema. Last-key
	// wins parsing is inappropriate for an operator-approved erasure instruction.
	keys := json.NewDecoder(bytes.NewReader(raw))
	start, err := keys.Token()
	if err != nil || start != json.Delim('{') {
		return receipt, errors.New("privacy: receipt must be a JSON object")
	}
	seen := map[string]bool{}
	for keys.More() {
		token, err := keys.Token()
		if err != nil {
			return receipt, err
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return receipt, errors.New("privacy: duplicate or invalid receipt key")
		}
		seen[key] = true
		var value json.RawMessage
		if err := keys.Decode(&value); err != nil {
			return receipt, err
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&receipt); err != nil {
		return receipt, fmt.Errorf("privacy: invalid receipt: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return receipt, errors.New("privacy: trailing receipt data")
	}
	if receipt.Version != 1 || receipt.PrincipalID == "" || receipt.AccountID == "" || receipt.InstanceID == "" || receipt.AppliedAt.IsZero() || (receipt.Action != "restrict" && receipt.Action != "erase") {
		return receipt, errors.New("privacy: receipt must name one instance/account/principal and a restrict or erase action")
	}
	return receipt, nil
}
