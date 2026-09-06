package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
)

// selfConfigOperations is closed deliberately: adding a feature to ordinary
// projects does not silently grant it control over the running instance.
var selfConfigOperations = map[Operation]bool{
	OpOrgGet: true, OpOrgRename: true, OpProjectGet: true, OpProjectList: true, OpProjectRename: true,
	OpEnvRead: true, OpEnvList: true, OpEnvRename: true, OpEnvReorder: true, OpEnvUpdateNote: true,
	OpKeyGet: true, OpKeyList: true, OpKeyGroupGet: true, OpKeyGroupList: true, OpFolderGet: true, OpFolderList: true,
	OpValueRead: true, OpValueList: true, OpValueReveal: true, OpRevealWindowRead: true,
	OpValueStage: true, OpValuePendingList: true, OpValuePublish: true,
	OpValueExport: true, OpValueExportReveal: true, OpValueExportRevealHistory: true,
	OpRevisionList: true, OpRevisionShow: true, OpRevisionSignals: true, OpRevisionRestore: true, OpRevisionRestoreHistory: true, OpRevisionRestoreCurrent: true,
	OpPinSet: true, OpPinSetHistory: true, OpPinList: true, OpPinRelease: true,
	OpApprovalPolicyRead: true, OpApprovalRequestRead: true, OpApprovalVote: true, OpApprovalBypass: true,
	OpDefinitionsExport: true, OpDefinitionsCheck: true, OpDefinitionsSettingsGet: true, OpProjectMachineRevealGet: true,
	OpEnvSettingsRead: true, OpOrgRetentionRead: true, OpOrgRetentionUpdate: true, OpProjectRetentionRead: true, OpProjectRetentionUpdate: true,
	OpAuditQueryOrg: true, OpAuditQueryProject: true, OpAuditQueryEnv: true, OpAuditExportOrg: true, OpAuditExportProject: true, OpAuditExportEnv: true,
	OpGrantCreateOrg: true, OpGrantCreateProject: true, OpGrantCreateEnv: true, OpGrantRevokeOrg: true, OpGrantRevokeProject: true, OpGrantRevokeEnv: true,
	OpGrantListOrg: true, OpGrantListProject: true, OpTemplateApplyOrg: true, OpTemplateApplyProject: true, OpTemplateApplyEnv: true, OpMemberInviteOrg: true,
	OpSelfConfigApply: true, OpSelfConfigTest: true, OpReencryptProject: true,
}

func selfConfigSessionEligible(caller Identity) bool {
	return (caller.Class == "" || caller.Class == domain.ClassHuman) &&
		(caller.SessionID == "" || AdequateAssurance(caller.Assurance))
}

func (a *TxAuthorizer) selfConfigProfile(ctx context.Context, caller Identity, op Operation, chain domain.Scope, grants []domain.Grant) (bool, bool, error) {
	protected, err := a.r.IsSelfConfigScope(ctx, chain)
	if err != nil || !protected {
		return protected, !protected && op != OpSelfConfigApply && op != OpSelfConfigTest, err
	}
	if caller.Class != "" && caller.Class != domain.ClassHuman {
		return true, false, nil
	}
	if !selfConfigOperations[op] {
		return true, false, nil
	}
	f := Formula{{Cap: domain.CapInstanceConfig, At: domain.LevelNone}}
	switch op {
	case OpGrantCreateOrg, OpGrantCreateProject, OpGrantCreateEnv, OpGrantRevokeOrg, OpGrantRevokeProject, OpGrantRevokeEnv, OpGrantListOrg, OpGrantListProject, OpTemplateApplyOrg, OpTemplateApplyProject, OpTemplateApplyEnv, OpMemberInviteOrg:
		f = append(f, Atom{Cap: domain.CapManageMembers, At: domain.LevelNone})
	}
	return true, evaluate(f, chain, grants), nil
}

// IsSelfConfig reports the database-resolved protected resource profile on a
// canonical proof. It is a value-handling hint, never a new authorization door.
func IsSelfConfig(p Proof) bool {
	v, ok := p.(*proof)
	return ok && v != nil && v.selfConfig && v.tok != nil && v.tok.alive()
}

// SelfConfigRuntimeAuthority selects only the local binding's retained snapshot.
// No network operation can mint this authority, even with valid admin grants.
// An empty snapshot ID selects the durable desired revision.
func (a *TxAuthorizer) SelfConfigRuntimeAuthority(ctx context.Context, snapshotID string) (Proof, error) {
	if operation.IsNetwork(ctx) {
		return nil, errors.New("authz: runtime authority is unavailable to network operations")
	}
	b, err := a.r.SelfConfigBinding(ctx)
	if err != nil {
		return nil, err
	}
	if snapshotID == "" {
		snapshotID = b.DesiredSnapshotID
	}
	retained, err := a.r.SelfConfigRetained(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	if !retained {
		return nil, domain.ErrNotFound
	}
	return &proof{kind: kindSystem, op: Operation("system:" + SiteSelfConfigRuntime), site: SiteSelfConfigRuntime, chain: b.Scope, tok: a.tok, selfConfig: true, runtimeSnapshotID: snapshotID}, nil
}

// VerifySelfConfigSnapshot confines a runtime proof's payload read to the one
// retained snapshot chosen at minting. Other proof classes keep their ordinary
// operation and tenant constraints.
func VerifySelfConfigSnapshot(p Proof, snapshotID string) error {
	c, ok := p.(*proof)
	if !ok || c == nil {
		return errors.New("authz: non-canonical proof")
	}
	if (c.site == SiteSelfConfigRuntime || c.site == SiteSelfConfigRecovery) && c.runtimeSnapshotID != snapshotID {
		return fmt.Errorf("authz: runtime proof does not address this snapshot")
	}
	return nil
}

// IncludesSelfConfig is the metadata enumeration projection of the caller's
// instance-config grant, resolved in the same transaction as the proof.
func IncludesSelfConfig(p Proof) bool {
	c, ok := p.(*proof)
	return ok && c != nil && c.selfConfigAdmin && c.tok != nil && c.tok.alive()
}

// SelfConfigRecoveryAuthority is a host-local, exact-revision recovery reader.
// It cannot use the runtime worker's retained-root privilege as a general read.
func (a *TxAuthorizer) SelfConfigRecoveryAuthority(ctx context.Context, revision int64) (Proof, error) {
	if operation.IsNetwork(ctx) {
		return nil, errors.New("authz: recovery authority is unavailable to network operations")
	}
	if revision < 1 {
		return nil, domain.ErrInvalid
	}
	b, err := a.r.SelfConfigBinding(ctx)
	if err != nil {
		return nil, err
	}
	snapshotID, err := a.r.SelfConfigRecoverySnapshot(ctx, b, revision)
	if err != nil {
		return nil, err
	}
	return &proof{kind: kindSystem, op: Operation("system:" + SiteSelfConfigRecovery), site: SiteSelfConfigRecovery, chain: b.Scope, tok: a.tok, selfConfig: true, runtimeSnapshotID: snapshotID}, nil
}
