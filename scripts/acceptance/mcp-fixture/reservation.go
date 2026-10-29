//go:build darwin || linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
)

type reservationReceipt struct {
	NamespaceUID string `json:"namespace_uid"`
	Resources    string `json:"resources_sha256"`
	Approval     string `json:"approval_ref"`
}
type storageVerification struct {
	PVCUID     string `json:"pvc_uid"`
	PVName     string `json:"pv_name"`
	RootMode   string `json:"root_mode"`
	RootGroup  int    `json:"root_group"`
	ChildMode  string `json:"private_child_mode"`
	ChildOwner int    `json:"private_child_owner"`
	Remount    bool   `json:"remount_preserves_private_child"`
	Locks      bool   `json:"cross_node_exclusive_locks_passed"`
	Reference  string `json:"operator_receipt_ref"`
}

func reservationControl(ctx context.Context, dir string) (publicControl, reservationReceipt, error) {
	var a executionAuthorization
	if err := authorizeDeployment(dir); err != nil {
		return publicControl{}, reservationReceipt{}, err
	}
	if err := readPrivate(filepath.Join(dir, "authorization.json"), &a); err != nil {
		return publicControl{}, reservationReceipt{}, err
	}
	c := publicControl{authorization: a}
	out, err := c.kubectl(ctx, "get", "namespace", "hikyo-mcp-651", "-o", "json").Output()
	var object struct {
		Metadata struct {
			UID    string            `json:"uid"`
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	}
	if err != nil || json.Unmarshal(out, &object) != nil || object.Metadata.UID == "" || object.Metadata.Labels["hikyo.dev/fixture"] != "mcp-651" {
		return c, reservationReceipt{}, errors.New("reserved namespace ownership unavailable")
	}
	return c, reservationReceipt{NamespaceUID: object.Metadata.UID, Resources: a.ResourcesSHA256, Approval: a.ApprovalRef}, nil
}

func recordReservation(ctx context.Context, dir string) error {
	_, r, err := reservationControl(ctx, dir)
	if err != nil {
		return err
	}
	return writePrivate(filepath.Join(dir, "reservation.json"), r)
}

func authorizeContinuation(ctx context.Context, dir string) error {
	c, current, err := reservationControl(ctx, dir)
	if err != nil {
		return err
	}
	var recorded reservationReceipt
	if err := readPrivate(filepath.Join(dir, "reservation.json"), &recorded); err != nil {
		return err
	}
	if current != recorded {
		return errors.New("refusing a changed or unowned namespace reservation")
	}
	proofPath := filepath.Join(dir, "storage-verification.json")
	if !sha256Pattern.MatchString(c.authorization.StorageSHA256) || fileHash(proofPath) != c.authorization.StorageSHA256 {
		return errors.New("HOLD: reviewed shared installation-authority storage proof required")
	}
	var proof storageVerification
	if err := readPrivate(proofPath, &proof); err != nil {
		return err
	}
	out, err := c.kubectl(ctx, "get", "pvc", "installation-authority", "-o", "json").Output()
	var claim struct {
		Metadata struct {
			UID string `json:"uid"`
		} `json:"metadata"`
		Spec struct {
			PV    string   `json:"volumeName"`
			Class string   `json:"storageClassName"`
			Modes []string `json:"accessModes"`
		} `json:"spec"`
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	}
	if err != nil || json.Unmarshal(out, &claim) != nil || claim.Status.Phase != "Bound" || claim.Spec.PV == "" || claim.Spec.Class != "nfs" || len(claim.Spec.Modes) != 1 || claim.Spec.Modes[0] != "ReadWriteMany" || proof.PVCUID != claim.Metadata.UID || proof.PVName != claim.Spec.PV || proof.RootMode != "2770" || proof.RootGroup != 65532 || proof.ChildMode != "0700" || proof.ChildOwner != 65532 || !proof.Remount || !proof.Locks || proof.Reference == "" {
		return errors.New("HOLD: shared storage identity, permissions, remount or cross-node lock proof does not match")
	}
	return nil
}
