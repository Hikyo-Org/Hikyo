//go:build darwin || linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/jackc/pgx/v5"
)

type executionAuthorization struct {
	ApprovedBy       string    `json:"approved_by"`
	ApprovalRef      string    `json:"approval_ref"`
	Expires          time.Time `json:"expires_at_utc"`
	Context          string    `json:"kube_context"`
	Namespace        string    `json:"namespace"`
	Origin           string    `json:"origin"`
	NoSpend          bool      `json:"no_new_spend"`
	NoProviders      bool      `json:"no_provider_calls"`
	FixtureMutations bool      `json:"synthetic_fixture_mutations"`
	Lifecycle        bool      `json:"fixture_lock_cancel_and_delete_one_server_pod"`
	ResourcesSHA256  string    `json:"rendered_resources_sha256"`
	IngressSHA256    string    `json:"ingress_verification_sha256"`
}

func authorizeDeployment(dir string) error {
	var a executionAuthorization
	var b deploymentBinding
	if err := readPrivate(filepath.Join(dir, "authorization.json"), &a); err != nil {
		return err
	}
	if err := readPrivate(filepath.Join(dir, "binding.json"), &b); err != nil {
		return err
	}
	if err := validateBinding(&b); err != nil {
		return err
	}
	if a.ApprovedBy == "" || a.ApprovalRef == "" || !a.Expires.After(time.Now()) || a.Expires.After(time.Now().Add(24*time.Hour)) || a.Context != "picluster" || a.Namespace != "hikyo-mcp-651" || a.Origin != b.Origin || !a.NoSpend || !a.NoProviders || !a.FixtureMutations || !a.Lifecycle || !sha256Pattern.MatchString(a.ResourcesSHA256) || fileHash(filepath.Join(dir, "resources.yaml")) != a.ResourcesSHA256 {
		return errors.New("HOLD: explicit current resource approval and exact rendered resource hash required")
	}
	if fileHash(filepath.Join(dir, "helper-linux-arm64")) != b.HelperBinary {
		return errors.New("selected helper bytes differ")
	}
	if fileHash(filepath.Join(dir, "artifact-verification.json")) != b.VerificationReceipt {
		return errors.New("authenticated artifact verification receipt missing or changed")
	}
	return validateDeploymentCustody(dir, b)
}

type observedPod struct {
	Metadata struct {
		UID    string            `json:"uid"`
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		Containers []struct {
			Image string `json:"image"`
		} `json:"containers"`
	} `json:"spec"`
	Status struct {
		IP         string `json:"podIP"`
		Phase      string `json:"phase"`
		Containers []struct {
			Ready   bool   `json:"ready"`
			ImageID string `json:"imageID"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

type publicControl struct{ authorization executionAuthorization }

const remoteRuntime = "/work/custody"

func (c publicControl) kubectl(ctx context.Context, args ...string) *exec.Cmd {
	base := []string{"--context", c.authorization.Context, "--namespace", c.authorization.Namespace}
	return exec.CommandContext(ctx, "kubectl", append(base, args...)...)
}

func (c publicControl) helper(ctx context.Context, action string) ([]byte, error) {
	out, err := c.kubectl(ctx, "exec", "tooling", "--", "/tools/mcp-fixture", action, "--runtime", remoteRuntime).Output()
	if err != nil {
		return nil, errors.New("fixture helper refused; protected operator diagnostics required")
	}
	return out, nil
}

func (c publicControl) pod(ctx context.Context, name string, b deploymentBinding) (observedPod, error) {
	var p observedPod
	out, err := c.kubectl(ctx, "get", "pod", name, "-o", "json").Output()
	if err != nil || json.Unmarshal(out, &p) != nil || p.Metadata.Name != name || p.Metadata.UID == "" || p.Metadata.Labels["hikyo.dev/fixture"] != "mcp-651" || p.Status.IP == "" || p.Status.Phase != "Running" || len(p.Spec.Containers) != 1 || len(p.Status.Containers) != 1 || !p.Status.Containers[0].Ready || p.Spec.Containers[0].Image != b.Image {
		return p, errors.New("fixture pod binding failed")
	}
	// Kubernetes sometimes reports an index rather than the selected child.
	// That is not enough for exact runtime platform proof; fail closed here.
	if !strings.HasSuffix(p.Status.Containers[0].ImageID, "@sha256:"+b.PlatformManifest) && p.Status.Containers[0].ImageID != "sha256:"+b.PlatformManifest {
		return p, errors.New("HOLD: runtime selected platform digest was not independently observed")
	}
	return p, nil
}

func publicCampaign(ctx context.Context, dir string) (result error) {
	if err := authorizeDeployment(dir); err != nil {
		return err
	}
	var a executionAuthorization
	var binding deploymentBinding
	var t target
	var f fixture
	for path, dst := range map[string]any{"authorization.json": &a, "binding.json": &binding, "target.json": &t, "fixture.json": &f} {
		if err := readPrivate(filepath.Join(dir, path), dst); err != nil {
			return err
		}
	}
	if err := validateBinding(&binding); err != nil {
		return err
	}
	if a.ApprovedBy == "" || a.ApprovalRef == "" || !a.Expires.After(time.Now()) || a.Expires.After(time.Now().Add(24*time.Hour)) || a.Context != "picluster" || a.Namespace != "hikyo-mcp-651" || a.Origin != binding.Origin || t.URL != binding.Origin+"/mcp" || t.CAFile != "" || !a.NoSpend || !a.NoProviders || !a.FixtureMutations || !a.Lifecycle {
		return errors.New("HOLD: current resource-specific deployment/campaign approval required")
	}
	ctx, cancel := context.WithDeadline(ctx, a.Expires)
	defer cancel()
	r := newReceipt()
	r.EvidenceClass = "public_fixture_campaign_only_not_release_acceptance"
	defer func() {
		if result != nil {
			r.Checks["outcome"] = "FAIL; HOLD; no public acceptance"
			if err := writePrivate(filepath.Join(dir, "public-failure-receipt.json"), r); err != nil {
				result = errors.Join(result, err)
			}
		}
	}()
	r.Identities = map[string]string{"source_commit": binding.SourceCommit, "source_tree": binding.SourceTree, "image": binding.Image, "platform_manifest_sha256": binding.PlatformManifest, "image_config_sha256": binding.ImageConfig, "server_binary_sha256": binding.Binary, "release_manifest_sha256": binding.Manifest, "operator_public_key_sha256": binding.OperatorPin, "verification_receipt_sha256": binding.VerificationReceipt}
	control := publicControl{authorization: a}
	grantRemoved := false
	defer func() {
		if !grantRemoved {
			return
		}
		if !time.Now().Before(a.Expires) {
			r.Checks["grant_recovery"] = "HOLD: execution window expired; approved cleanup owner must restore"
			return
		}
		recovery, cancel := context.WithDeadline(context.Background(), a.Expires)
		defer cancel()
		if _, err := control.helper(recovery, "grant-restore"); err != nil {
			result = errors.Join(result, errors.New("supported grant recovery failed; owner action required"))
			r.Checks["grant_recovery"] = "FAIL"
		} else {
			r.Checks["grant_recovery"] = "PASS: restored through supported writer after interrupted campaign"
			r.event("failure_recovery_grant_restoration_committed", "", 0)
		}
	}()
	helperHash, err := control.kubectl(ctx, "exec", "tooling", "--", "sha256sum", "/tools/mcp-fixture").Output()
	if err != nil || len(strings.Fields(string(helperHash))) != 2 || strings.Fields(string(helperHash))[0] != binding.HelperBinary {
		return errors.New("remote helper bytes differ from reviewed binding")
	}
	pods := make([]observedPod, 2)
	for i := 0; i < 2; i++ {
		pod, err := control.pod(ctx, fmt.Sprintf("hikyo-%d", i), binding)
		if err != nil {
			return err
		}
		pods[i] = pod
	}
	if pods[0].Metadata.UID == pods[1].Metadata.UID || pods[0].Status.IP == pods[1].Status.IP {
		return errors.New("two distinct runtime processes required")
	}
	// These IDs come from the authenticated cluster inventory, not the
	// response header alone. Receipt hashes keep private addresses out.
	t.Processes = []string{pods[0].Status.IP + ":8443", pods[1].Status.IP + ":8443"}
	p, err := newProbe(t, f, r)
	if err != nil {
		return err
	}
	defer p.client.CloseIdleConnections()
	if err := p.publicPreflight(ctx, dir, a, binding, pods); err != nil {
		return err
	}
	if err := p.baseline(ctx); err != nil {
		return err
	}
	for i := 0; i < 2; i++ {
		v, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, p.args(), f.Credentials["rotating"].Token)
		if err != nil || !successful(v) {
			return errors.New("predecessor live control failed")
		}
	}
	if _, err := control.helper(ctx, "revoke"); err != nil {
		return err
	}
	r.event("credential_revocation_committed_writer_return", "", 0)
	if err := p.firstDenials(ctx, f.Credentials["rotating"].Token, true); err != nil {
		return err
	}
	r.Checks["supported_committed_revocation_first_call_each_process_replacement"] = "PASS"
	if _, err := control.helper(ctx, "grant-remove"); err != nil {
		return err
	}
	grantRemoved = true
	r.event("grant_removal_committed_writer_return", "", 0)
	if err := p.firstDenials(ctx, f.Credentials["live"].Token, false); err != nil {
		return err
	}
	if _, err := control.helper(ctx, "grant-restore"); err != nil {
		return err
	}
	grantRemoved = false
	r.event("grant_restoration_committed_writer_return", "", 0)
	for i := 0; i < 2; i++ {
		v, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, p.args(), f.Credentials["live"].Token)
		if err != nil || !successful(v) {
			return errors.New("public same-bearer recovery failed")
		}
	}
	r.Checks["supported_grant_remove_restore_both"] = "PASS"
	if err := p.smoke(ctx, dir, func() error { _, err := control.helper(ctx, "revoke-smoke"); return err }); err != nil {
		return err
	}
	if err := p.namedClients(ctx, dir); err != nil {
		return err
	}
	if err := p.publicLifecycle(ctx, control, pods, binding); err != nil {
		return err
	}
	wireRaw, err := control.kubectl(ctx, "exec", "deployment/gateway", "--", "cat", "/work/custody/wire-receipt.json").Output()
	var wire []wireObservation
	if err != nil || json.Unmarshal(wireRaw, &wire) != nil || !validCodexWire(wire) {
		return errors.New("public Codex requested/negotiated wire receipt missing")
	}
	if err := writePrivate(filepath.Join(dir, "wire-receipt.json"), wire); err != nil {
		return err
	}
	r.Checks["named_client_wire_protocols"] = "PASS: Go/Inspector client receipts and Codex gateway requested/negotiated 2025-06-18"
	r.Checks["direct_TLS_comparison"] = "NOTRUN: retain a separate authorized direct-TLS observation"
	r.Checks["open_stream"] = "NOT_APPLICABLE: bounded JSON profile; no open-stream acceptance claim"
	r.Checks["release_acceptance"] = "HOLD: coordinator must verify artifact receipts, public origin, wire observations, independent vantage and cleanup"
	return writePrivate(filepath.Join(dir, "public-receipt.json"), r)
}

type storeCounts struct {
	Claims  int   `json:"claims"`
	Blocked []int `json:"blocked_catalogue_queries"`
}

// Observations deliberately use a read-only connection after the exact DSN
// guard. Re-running boot admission while the catalogue lock is held could
// itself wait behind the lock and conceal the cancellation being measured.
func observeReadOnly(ctx context.Context, cfg settings) error {
	if err := validateDSN(cfg.DSN, cfg.Development); err != nil {
		return err
	}
	connection, err := pgx.Connect(ctx, cfg.DSN)
	if err != nil {
		return errors.New("read-only observation connection failed")
	}
	defer connection.Close(context.Background())
	tx, err := connection.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return errors.New("read-only observation transaction failed")
	}
	defer tx.Rollback(context.Background())
	c := storeCounts{Blocked: make([]int, 2)}
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_inflight").Scan(&c.Claims); err != nil {
		return errors.New("claim observation failed")
	}
	for i := 0; i < 2; i++ {
		if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock' AND query LIKE '%ListKeysPage%'", fmt.Sprintf("hikyo-%d", i)).Scan(&c.Blocked[i]); err != nil {
			return errors.New("store context observation failed")
		}
	}
	return json.NewEncoder(os.Stdout).Encode(c)
}

func storeObservation(ctx context.Context, db *store.DB, action string) error {
	if action == "hold" {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		lock, err := db.PG().Begin(ctx)
		if err != nil {
			return errors.New("fixture lock unavailable")
		}
		defer lock.Rollback(context.Background())
		if _, err := lock.Exec(ctx, "LOCK TABLE keys IN ACCESS EXCLUSIVE MODE"); err != nil {
			return errors.New("fixture lock refused")
		}
		fmt.Fprintln(os.Stdout, "LOCKED")
		done := make(chan struct{})
		go func() { _, _ = bufio.NewReader(os.Stdin).ReadByte(); close(done) }()
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return errors.New("fixture lock expired")
		}
	}
	c := storeCounts{Blocked: make([]int, 2)}
	if err := db.PG().QueryRow(ctx, "SELECT COUNT(*) FROM mcp_inflight").Scan(&c.Claims); err != nil {
		return errors.New("claim observation failed")
	}
	for i := 0; i < 2; i++ {
		if err := db.PG().QueryRow(ctx, "SELECT COUNT(*) FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock' AND query LIKE '%ListKeysPage%'", fmt.Sprintf("hikyo-%d", i)).Scan(&c.Blocked[i]); err != nil {
			return errors.New("store context observation failed")
		}
	}
	return json.NewEncoder(os.Stdout).Encode(c)
}

func (c publicControl) counts(ctx context.Context) (storeCounts, error) {
	var v storeCounts
	b, err := c.helper(ctx, "observe")
	if err != nil {
		return v, err
	}
	if json.Unmarshal(b, &v) != nil || len(v.Blocked) != 2 {
		return v, errors.New("invalid store observation")
	}
	return v, nil
}

func (c publicControl) lock(ctx context.Context) (func() error, error) {
	cmd := c.kubectl(ctx, "exec", "-i", "tooling", "--", "/tools/mcp-fixture", "hold", "--runtime", remoteRuntime)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if cmd.Start() != nil {
		return nil, errors.New("store lock helper launch failed")
	}
	var line bytes.Buffer
	for line.Len() < 16 {
		var b [1]byte
		if _, err := out.Read(b[:]); err != nil {
			in.Close()
			cmd.Wait()
			return nil, errors.New("store lock receipt missing")
		}
		if b[0] == '\n' {
			break
		}
		line.Write(b[:])
	}
	if line.String() != "LOCKED" {
		in.Close()
		cmd.Wait()
		return nil, errors.New("store lock not established")
	}
	released := false
	return func() error {
		if released {
			return nil
		}
		released = true
		in.Close()
		return cmd.Wait()
	}, nil
}

func (p *probe) publicLifecycle(ctx context.Context, c publicControl, pods []observedPod, binding deploymentBinding) error {
	seen := map[int]bool{}
	for attempt := 0; attempt < 4 && len(seen) < 2; attempt++ {
		release, err := c.lock(ctx)
		if err != nil {
			return err
		}
		callCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() {
			_, err := p.call(callCtx, "tools/call", mcpserver.ToolListDefinitions, p.args(), p.fixture.Credentials["lifecycle"].Token)
			done <- err
		}()
		observed := -1
		if err := wait(ctx, 4*time.Second, func() bool {
			v, err := c.counts(ctx)
			if err != nil || v.Claims != 1 {
				return false
			}
			for i, n := range v.Blocked {
				if n > 0 {
					observed = i
					return true
				}
			}
			return false
		}); err != nil {
			cancel()
			release()
			<-done
			return errors.New("public admitted store work not observed")
		}
		start := time.Now()
		cancel()
		<-done
		if err := wait(ctx, 3*time.Second, func() bool { v, err := c.counts(ctx); return err == nil && v.Claims == 0 && v.Blocked[observed] == 0 }); err != nil {
			release()
			return errors.New("public cancellation left a store query or shared claim")
		}
		p.receipt.Measurements[fmt.Sprintf("public_proxy_cancel_process_%d", observed)] = time.Since(start).Milliseconds()
		if err := release(); err != nil {
			return errors.New("owned lock cleanup failed")
		}
		seen[observed] = true
	}
	if len(seen) != 2 {
		return errors.New("public cancellation did not cover both independently observed processes")
	}
	p.receipt.Checks["public_proxy_cancellation_store_context_claim_release_both"] = "PASS"
	// A Kubernetes deletion sends SIGTERM to this exact observed pod. Zero
	// grace, forced deletion and changing the server's grace are forbidden.
	release, err := c.lock(ctx)
	if err != nil {
		return err
	}
	defer release()
	done := make(chan error, 1)
	go func() {
		r, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, p.args(), p.fixture.Credentials["lifecycle"].Token)
		if err == nil && !successful(r) {
			err = errors.New("draining public read failed")
		}
		done <- err
	}()
	index := -1
	if err := wait(ctx, 4*time.Second, func() bool {
		v, err := c.counts(ctx)
		if err != nil || v.Claims != 1 {
			return false
		}
		for i, n := range v.Blocked {
			if n > 0 {
				index = i
				return true
			}
		}
		return false
	}); err != nil {
		return errors.New("public drain admission not observed")
	}
	current, err := c.pod(ctx, pods[index].Metadata.Name, binding)
	if err != nil || current.Metadata.UID != pods[index].Metadata.UID {
		return errors.New("refusing changed pod before drain")
	}
	start := time.Now()
	deletion := c.kubectl(ctx, "delete", "--raw", "/api/v1/namespaces/hikyo-mcp-651/pods/"+current.Metadata.Name, "-f", "-")
	preconditions, _ := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": map[string]string{"uid": current.Metadata.UID}})
	deletion.Stdin = bytes.NewReader(preconditions)
	if _, err := deletion.Output(); err != nil {
		return errors.New("fixture SIGTERM deletion refused")
	}
	if err := release(); err != nil {
		return errors.New("drain lock release failed")
	}
	select {
	case err := <-done:
		if err != nil {
			return err
		}
	case <-time.After(5 * time.Second):
		return errors.New("public admitted read exceeded drain budget")
	}
	if err := wait(ctx, time.Until(start.Add(5*time.Second)), func() bool { v, err := c.counts(ctx); return err == nil && v.Claims == 0 }); err != nil {
		return errors.New("public drain left shared claims")
	}
	peer, err := c.pod(ctx, pods[1-index].Metadata.Name, binding)
	if err != nil || peer.Metadata.UID != pods[1-index].Metadata.UID {
		return errors.New("drain peer changed or failed")
	}
	r, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, p.args(), p.fixture.Credentials["lifecycle"].Token)
	if err != nil || !successful(r) || r.Process != p.target.Processes[1-index] {
		return errors.New("public peer positive control failed")
	}
	p.receipt.Measurements["public_SIGTERM_admitted_read_and_claim_release"] = time.Since(start).Milliseconds()
	if err := wait(ctx, time.Until(start.Add(5*time.Second)), func() bool {
		out, err := c.kubectl(ctx, "get", "pod", current.Metadata.Name, "--ignore-not-found=true", "-o", "json").Output()
		if err != nil {
			return false
		}
		if len(bytes.TrimSpace(out)) == 0 {
			return true
		}
		var replacement observedPod
		return json.Unmarshal(out, &replacement) == nil && replacement.Metadata.UID != "" && replacement.Metadata.UID != current.Metadata.UID
	}); err != nil {
		return errors.New("old graceful pod removal not observed within unchanged five-second budget")
	}
	p.receipt.Checks["public_SIGTERM_process_exit"] = "PASS: UID-preconditioned graceful deletion and old pod removal observed within five seconds; attach runtime termination record"
	var replacement observedPod
	if err := wait(ctx, 120*time.Second, func() bool {
		pod, err := c.pod(ctx, current.Metadata.Name, binding)
		if err != nil || pod.Metadata.UID == current.Metadata.UID {
			return false
		}
		replacement = pod
		return true
	}); err != nil {
		return errors.New("replacement process failed selected-image recovery")
	}
	p.target.Processes[index] = replacement.Status.IP + ":8443"
	seenRecovery := map[string]bool{}
	for i := 0; i < 2; i++ {
		reply, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, p.args(), p.fixture.Credentials["live"].Token)
		if err != nil || !successful(reply) || !contains(p.target.Processes, reply.Process) {
			return errors.New("public post-drain recovery failed")
		}
		seenRecovery[reply.Process] = true
	}
	if len(seenRecovery) != 2 {
		return errors.New("public recovery missed a process")
	}
	p.receipt.Checks["public_replacement_and_peer_same_custody_recovery"] = "PASS"
	return nil
}
