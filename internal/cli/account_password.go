package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// requireLocalAccountProof refuses ceremonies that need browser-bound establish
// evidence before requesting a secret from the controlling terminal.
func requireLocalAccountProof(ctx context.Context, client *Client, command string) (apigen.TotpStatus, error) {
	var status apigen.TotpStatus
	if err := client.Do(ctx, http.MethodGet, api.PathPrefix+"/auth/totp", nil, &status); err != nil {
		return status, err
	}
	if !status.HasPassword && !status.Confirmed && !status.HasPasskey {
		return status, failf(ExitRefused, "`%s` is not available on a social-only account: verify with your provider in Settings > Security before creating a local credential: %s", command, accountSecurityURL(client))
	}
	return status, nil
}

func accountSecurityURL(client *Client) string {
	return strings.TrimRight(client.Entry.Origin, "/") + "/settings?tab=factors"
}

func runAccountPassword(ctx context.Context, ios IO, args []string) error {
	if len(args) == 0 || (args[0] != "set" && args[0] != "change") {
		return failf(ExitUsage, "usage: hikyo account password set|change")
	}
	command := "account password " + args[0]
	st, flags, err := parseCommon(command, ios, args[1:], nil)
	if err != nil {
		return err
	}
	if err := flags.checkNoPositionals(command); err != nil {
		return err
	}
	client, artifact, err := authenticatedClient(st, ios, flags)
	if err != nil {
		return err
	}
	session, err := requireHumanSession("hikyo "+command, artifact)
	if err != nil {
		return err
	}
	status, err := requireLocalAccountProof(ctx, client, command)
	if err != nil {
		return err
	}
	if !status.HasPassword && !status.Confirmed {
		return failf(ExitRefused, "`%s` requires an existing password or TOTP proof. This account has neither; contact an administrator for credential recovery. Review Settings > Security: %s", command, accountSecurityURL(client))
	}
	proofPrompt := "Current password (to authorize the password change): "
	if !status.HasPassword {
		proofPrompt = "TOTP code (to authorize setting a password): "
	}
	proof, err := ios.readPassword(proofPrompt)
	if err != nil {
		return err
	}
	password, err := ios.readPassword("New password (minimum 12 characters): ")
	if err != nil {
		return err
	}
	confirm, err := ios.readPassword("Repeat the password: ")
	if err != nil {
		return err
	}
	if password != confirm {
		return failf(ExitRefused, "the two passwords do not match")
	}
	var result apigen.LoginResult
	if err := client.Do(ctx, http.MethodPut, api.PathPrefix+"/auth/password", apigen.SetPasswordRequest{Password: password, Proof: &proof}, &result); err != nil {
		return err
	}
	if err := persistRotatedSession(st, session, result); err != nil {
		return err
	}
	fmt.Fprintln(ios.Stderr, "password saved; the current session was replaced and other sessions were revoked")
	return nil
}
