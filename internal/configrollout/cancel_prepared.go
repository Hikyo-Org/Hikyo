package configrollout

import "context"

// cancelPrepared proves that this exact preparation has made no external
// changes. The controller journals the terminal response. No request, rollback,
// configuration or Deployment write is needed to cancel an unseen Submit.
func (k *Kubernetes) cancelPrepared(ctx context.Context, intent Intent, expectedPlanDigest string, plan *Plan) (Receipt, error) {
	if plan == nil || !validIntent(intent) || !validDigest(expectedPlanDigest) || plan.data.Intent != intent || plan.data.TargetDigest != k.targetDigest || plan.digest != expectedPlanDigest || digest(plan.data) != expectedPlanDigest || !k.validPlan(plan.data) {
		return Receipt{}, ErrInvalid
	}
	d, secrets, err := k.get(ctx)
	if err != nil {
		return Receipt{}, err
	}
	if !compatibleUIDs(plan.data, d, secrets, k.target) || digest(d.Spec) != plan.data.BeforeSpecDigest || version(secrets[k.target.ConfigSecret]) != plan.data.Resources.Config || !sameData(secrets[k.target.ConfigSecret].Data, plan.data.ConfigBefore) {
		return Receipt{}, ErrConflict
	}
	// Status-only Deployment RV changes do not alter the input baseline. Module
	// records may contain this Submit's interrupted bookkeeping, but nothing
	// else may have changed. Configuration itself has not been written yet.
	request := secrets[k.target.RequestSecret]
	ownedRequest := false
	if version(request) != plan.data.Resources.Request {
		var saved record
		if decode(request.Data[requestKey], &saved) != nil || saved.Digest != expectedPlanDigest || digest(saved.Plan) != expectedPlanDigest {
			return Receipt{}, ErrConflict
		}
		ownedRequest = true
	}
	rollback := secrets[k.target.RollbackSecret]
	if version(rollback) != plan.data.Resources.Rollback {
		var saved rollbackRecord
		if !ownedRequest || decode(rollback.Data[rollbackKey], &saved) != nil || saved.Intent != intent || saved.Digest != expectedPlanDigest || !sameData(saved.Config, plan.data.ConfigBefore) || digest(saved.Delta) != digest(plan.data.Delta) {
			return Receipt{}, ErrConflict
		}
	}
	storedReceipt := secrets[k.target.ReceiptSecret]
	if version(storedReceipt) != plan.data.Resources.Receipt {
		var saved Receipt
		if !ownedRequest || decode(storedReceipt.Data[receiptKey], &saved) != nil || saved.Intent != intent || saved.PlanDigest != expectedPlanDigest || saved.DeploymentUID != d.UID || saved.ApplicationAcknowledged || saved.Phase != Prepared && saved.Phase != Restored {
			return Receipt{}, ErrConflict
		}
	}
	receipt := Receipt{Intent: intent, PlanDigest: expectedPlanDigest, DeploymentUID: d.UID, Phase: Restored, Resources: k.resources(d, secrets)}
	if ownedRequest {
		// An interrupted Submit left a module request. Mark that exact record
		// terminal so the next authorized plan can reuse the fixed storage slot.
		return k.saveReceipt(ctx, storedReceipt, receipt)
	}
	return receipt, nil
}
