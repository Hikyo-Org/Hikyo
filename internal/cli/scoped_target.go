package cli

import (
	"net/url"

	"github.com/Hikyo-Org/hikyo/api"
)

// scopedTarget is ready for a command's requests. base addresses its resource
// parent; environment is also retained when the request body needs it. The
// resolved dimensions and their validation order stay inside preparation.
type scopedTarget struct {
	client      *Client
	artifact    AuthArtifact
	base        string
	environment string
}

// prepareScopedTarget prepares the parameter, retention, SSH and adapter-target
// command family. Authentication, including artifact policy, origin binding and
// target echo, remains canonical in authenticatedTarget and precedes required
// scope validation. In particular, a missing session must still win over a
// missing org, and successful authentication must still echo before that error.
// DEK and machine delivery commands have different contracts and do not use it.
func prepareScopedTarget(st *State, ios IO, flags commonFlags) (scopedTarget, error) {
	client, artifact, resolved, err := authenticatedTarget(st, ios, flags)
	if err != nil {
		return scopedTarget{}, err
	}
	scope, err := scopedTargetDepth(flags.operation)
	if err != nil {
		return scopedTarget{}, err
	}
	org, err := resolved.Require(DimOrg)
	if err != nil {
		return scopedTarget{}, err
	}
	target := scopedTarget{client: client, artifact: artifact, base: api.PathPrefix + "/orgs/" + url.PathEscape(org)}
	if scope == TenantScopeOrg {
		return target, nil
	}
	project, err := resolved.Require(DimProject)
	if err != nil {
		return scopedTarget{}, err
	}
	target.base += "/projects/" + url.PathEscape(project)
	if scope == TenantScopeProject {
		return target, nil
	}
	target.environment, err = resolved.Require(DimEnv)
	if err != nil {
		return scopedTarget{}, err
	}
	// Adapter targets belong to the project, but adding one also requires an
	// environment in its body. Validate it before provider lookup or ceremony.
	if flags.operation != "adapter target add" {
		target.base += "/environments/" + url.PathEscape(target.environment)
	}
	return target, nil
}

// Required depth is a property of the existing operation, not another mode its
// caller must keep in sync with authentication and path construction. Surplus
// resolved dimensions remain in the echo without changing the addressed scope.
func scopedTargetDepth(operation AuthOperation) (TenantScopeKind, error) {
	switch operation {
	case "org retention get", "org retention set":
		return TenantScopeOrg, nil
	case "project retention get", "project retention set",
		"adapter target list", "adapter target show", "adapter target remove",
		"adapter target pause", "adapter target resume",
		"adapter plan", "adapter sync", "adapter test", "adapter adopt":
		return TenantScopeProject, nil
	case "env param list", "env param add", "env param delete", "adapter target add",
		"ssh-ca create", "ssh-ca list", "ssh-ca show", "ssh-ca rotate",
		"ssh-ca retire-key", "ssh-ca delete", "ssh-ca trusted-keys", "ssh-ca krl",
		"ssh-profile create", "ssh-profile update", "ssh-profile list", "ssh-profile show", "ssh-profile delete",
		"ssh-cert issue", "ssh-cert list", "ssh-cert show", "ssh-cert revoke":
		return TenantScopeEnvironment, nil
	default:
		return TenantScopeInstance, failf(ExitInternal, "hikyo %s has no scoped-target preparation rule", operation)
	}
}
