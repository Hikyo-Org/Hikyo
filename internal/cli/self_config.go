package cli

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func runSelfConfig(ctx context.Context, ios IO, sub string, args []string) error {
	var format, to, idempotency string
	var revision, expected int64
	var confirm, confirmRestored bool
	state, flags, err := parseCommon("instance-config "+sub, ios, args, func(fs *flag.FlagSet) {
		fs.StringVar(&format, "o", "table", "output format: table or json")
		fs.Int64Var(&revision, "revision", 0, "exact published revision")
		fs.Int64Var(&expected, "expected-generation", -1, "generation observed before this decision")
		fs.StringVar(&to, "to", "", "test email recipient")
		fs.StringVar(&idempotency, "idempotency-key", "", "stable key for retrying this exact apply")
		fs.BoolVar(&confirm, "yes", false, "confirm adoption after preview")
		fs.BoolVar(&confirmRestored, "confirm-restored-credentials", false, "confirm reviewed credentials and reconciled access grants after restore")
	})
	if err != nil {
		return err
	}
	if err := flags.checkNoPositionals("instance-config " + sub); err != nil {
		return err
	}
	output, err := ParseFormat(format)
	if err != nil {
		return err
	}
	client, artifact, _, err := authenticatedTarget(state, ios, flags)
	if err != nil {
		return err
	}
	human, err := requireHumanSession("managed instance configuration", artifact)
	if err != nil {
		return err
	}
	var status apigen.InstanceConfigStatus
	if err := client.Do(ctx, http.MethodGet, "/api/v1/instance/config", nil, &status); err != nil {
		return err
	}
	if confirmRestored && sub != "apply" {
		return failf(ExitUsage, "--confirm-restored-credentials is only valid for apply")
	}
	if sub == "status" {
		return renderSelfConfig(ios, output, status)
	}
	target := apigen.SelfConfigReauthIntent{OwnerInstanceId: status.OwnerInstanceId, Action: apigen.SelfConfigReauthIntentAction(sub), Revision: revision, ExpectedGeneration: expected, To: to, ConfirmRestoredCredentials: confirmRestored}
	var binding apigen.InstanceConfigBinding
	if status.Managed {
		if status.Binding == nil {
			return failf(ExitInternal, "managed configuration has no binding")
		}
		binding = *status.Binding
		target.SchemaVersion = binding.SchemaVersion
	}
	operation := "self-config.apply"
	if idempotency == "" {
		idempotency = rand.Text()
	}
	switch sub {
	case "adopt":
		var preview apigen.InstanceConfigAdoptionPreview
		if err := client.Do(ctx, http.MethodGet, "/api/v1/instance/config/adoption", nil, &preview); err != nil {
			return err
		}
		if !confirm {
			return Render(ios.Stdout, output, Table{Columns: []string{"OWNER", "KEY"}, Rows: adoptionRows(preview), JSON: preview})
		}
		fmt.Fprintf(ios.Stderr, "Adopt configuration on %s. Imported key names: %v.\n", preview.OwnerInstanceId, preview.ConfiguredKeys)
		target.Action = "adopt"
		target.SchemaVersion = preview.SchemaVersion
		target.Revision = 0
		target.ExpectedGeneration = 0
		target.PreviewToken = preview.PreviewToken
		target.To = ""
		operation = "self-config.adopt"
	case "apply", "test-email":
		if !status.Managed || revision < 1 || expected < 0 {
			return failf(ExitUsage, "%s needs a managed project, --revision N and --expected-generation N", sub)
		}
		if sub == "test-email" {
			if to == "" {
				return failf(ExitUsage, "test-email needs --to ADDRESS")
			}
			target.Action = "mail-test"
			operation = "self-config.test"
		} else if to != "" {
			return failf(ExitUsage, "--to is only valid for test-email")
		}
	default:
		return failf(ExitUsage, "unknown configuration action %q", sub)
	}
	fmt.Fprintf(ios.Stderr, "Authorize %s on owner %s, revision %d, generation %d in the browser.\n", target.Action, target.OwnerInstanceId, target.Revision, target.ExpectedGeneration)
	if err := runCLIReauthHandoffTarget(ctx, client, state, human, "self-config", operation, nil, nil, &target, ios.OpenURL); err != nil {
		return err
	}
	switch sub {
	case "adopt":
		err = client.Do(ctx, http.MethodPost, "/api/v1/instance/config/adoption", apigen.InstanceConfigAdoptRequest{PreviewToken: target.PreviewToken, IdempotencyKey: idempotency}, &status)
	case "apply":
		err = client.Do(ctx, http.MethodPost, "/api/v1/instance/config/apply", apigen.InstanceConfigApplyRequest{Revision: revision, ExpectedGeneration: expected, SchemaVersion: target.SchemaVersion, IdempotencyKey: idempotency, ConfirmRestoredCredentials: confirmRestored}, &status)
	case "test-email":
		var result apigen.InstanceConfigMailTestResult
		err = client.Do(ctx, http.MethodPost, "/api/v1/instance/config/mail/test", apigen.InstanceConfigMailTestRequest{Revision: revision, ExpectedGeneration: expected, SchemaVersion: target.SchemaVersion, To: openapi_types.Email(to)}, &result)
		if err != nil {
			return err
		}
		return Render(ios.Stdout, output, Table{Columns: []string{"REVISION", "SENT"}, Rows: [][]string{{strconv.FormatInt(result.Revision, 10), strconv.FormatBool(result.Sent)}}, JSON: result})
	}
	if err != nil {
		return err
	}
	if sub == "adopt" {
		fmt.Fprintln(ios.Stderr, "Adoption created scoped grants. Sign in again to use the new authority.")
	}
	return renderSelfConfig(ios, output, status)
}

func renderSelfConfig(ios IO, format Format, status apigen.InstanceConfigStatus) error {
	desired := "none"
	if status.DesiredRevision != nil {
		desired = strconv.FormatInt(*status.DesiredRevision, 10)
	}
	return Render(ios.Stdout, format, Table{Columns: []string{"OWNER", "STATE", "GENERATION", "DESIRED REVISION"}, Rows: [][]string{{status.OwnerInstanceId, string(status.State), strconv.FormatInt(status.Generation, 10), desired}}, JSON: status})
}
func adoptionRows(preview apigen.InstanceConfigAdoptionPreview) [][]string {
	rows := make([][]string, 0, len(preview.ConfiguredKeys))
	for _, key := range preview.ConfiguredKeys {
		rows = append(rows, []string{preview.OwnerInstanceId, key})
	}
	return rows
}
