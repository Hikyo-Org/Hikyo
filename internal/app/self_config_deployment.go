package app

import (
	"bytes"
	"context"
	stdcrypto "crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/configrollout"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const deploymentSourcesDirectory = "/run/hikyo/rollout/sources"
const deploymentSelectionDirectory = "/run/hikyo/rollout/selection"

// bootstrapDeployment has only the installed command/response mailbox's API
// authority. Alias contents come from fixed read-only projections, never from
// Hikyo values, request paths, or a Kubernetes Secret-read capability.
type bootstrapDeployment struct {
	db                                   *store.DB
	keyring                              *crypto.Keyring
	cfg                                  *config.Config
	enrollment                           configrollout.Enrollment
	signer                               stdcrypto.Signer
	mailbox                              *configrollout.Mailbox
	identity                             service.DeploymentIdentity
	installed                            config.ManagedBootstrapSources
	sourcesDirectory, selectionDirectory string
	mu                                   sync.Mutex
	proofs                               map[string]deploymentSourceProof
}

type deploymentSourceProof struct {
	database *store.VerifiedPostgresSource
	root     [32]byte
	epoch    uint32
	wrapper  *crypto.WrappedKey
	expires  time.Time
}

var _ service.BootstrapDeployment = (*bootstrapDeployment)(nil)

func configureBootstrapDeployment(ctx context.Context, cfg *config.Config, db *store.DB, kr *crypto.Keyring) (service.BootstrapDeployment, error) {
	if cfg == nil {
		return nil, configrollout.ErrUnavailable
	}
	if cfg.ConfigRolloutEnrollment == "" && cfg.ConfigRolloutSigningKey == "" {
		return nil, nil
	}
	if cfg.HA {
		return nil, configrollout.ErrUnsupported
	}
	if db == nil || kr == nil || cfg.ConfigRolloutEnrollment == "" || cfg.ConfigRolloutSigningKey == "" {
		return nil, configrollout.ErrUnavailable
	}
	raw, err := readDeploymentFile(cfg.ConfigRolloutEnrollment, false)
	if err != nil {
		return nil, configrollout.ErrUnavailable
	}
	enrollment, err := configrollout.ParseEnrollment(raw)
	if err != nil {
		return nil, configrollout.ErrUnavailable
	}
	owner, incarnation, err := db.RecoveryIdentity()
	if err != nil || owner != enrollment.OwnerInstanceID || incarnation != enrollment.Incarnation {
		return nil, configrollout.ErrConflict
	}
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = "local"
	}
	if nodeID != enrollment.Target.StableNodeID {
		return nil, configrollout.ErrConflict
	}
	signer, err := readDeploymentSigner(cfg.ConfigRolloutSigningKey)
	if err != nil {
		return nil, configrollout.ErrUnavailable
	}
	clientConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, configrollout.ErrUnavailable
	}
	client, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, configrollout.ErrUnavailable
	}
	mailbox, err := configrollout.NewMailbox(client, enrollment)
	if err != nil {
		return nil, configrollout.ErrUnavailable
	}
	copy := *cfg
	provider := &bootstrapDeployment{
		db: db, keyring: kr, cfg: &copy, enrollment: enrollment, signer: signer, mailbox: mailbox,
		sourcesDirectory: deploymentSourcesDirectory, selectionDirectory: deploymentSelectionDirectory,
		proofs:   make(map[string]deploymentSourceProof),
		identity: service.DeploymentIdentity{EnrollmentID: enrollment.ID, OwnerInstanceID: owner, Incarnation: incarnation, DeploymentUID: string(enrollment.Target.DeploymentUID)},
	}
	selected, stamp, err := provider.readSelection()
	if err != nil {
		return nil, err
	}
	provider.installed = selected
	provider.identity.TemplateStamp = stamp
	if len(enrollment.Target.DatabaseSources) > 0 && selected.DatabaseSource == "" || len(enrollment.Target.RootSources) > 0 && selected.RootSource == "" {
		return nil, configrollout.ErrUnavailable
	}
	if err := db.CheckAdmission(ctx); err != nil {
		return nil, configrollout.ErrUnavailable
	}
	return provider, nil
}

func (d *bootstrapDeployment) Identity() service.DeploymentIdentity { return d.identity }

func (d *bootstrapDeployment) SeedSources(ctx context.Context) (config.ManagedBootstrapSources, error) {
	if err := d.verifySelections(ctx, d.installed); err != nil {
		return config.ManagedBootstrapSources{}, err
	}
	return d.installed, nil
}

func (d *bootstrapDeployment) PrepareCommand(ctx context.Context, intent configrollout.Intent, bundle *runtimeconfig.Bundle, sequence uint64) (configrollout.SignedCommand, error) {
	if bundle == nil {
		return configrollout.SignedCommand{}, configrollout.ErrInvalid
	}
	selected := bundle.BootstrapSources()
	if selected.Version != 1 {
		return configrollout.SignedCommand{}, configrollout.ErrInvalid
	}
	changes := &configrollout.BootstrapChanges{}
	if selected.DatabaseSource != "" && selected.DatabaseSource != d.installed.DatabaseSource {
		source, ok := d.enrollment.Target.DatabaseSources[selected.DatabaseSource]
		if !ok {
			return configrollout.SignedCommand{}, configrollout.ErrUnsupported
		}
		dsn, err := d.databaseSource(selected.DatabaseSource)
		if err != nil {
			return configrollout.SignedCommand{}, err
		}
		proof, err := d.db.VerifyPostgresSource(ctx, dsn)
		if err != nil {
			return configrollout.SignedCommand{}, configrollout.ErrUnavailable
		}
		changes.Database = &configrollout.SourceProof{Alias: selected.DatabaseSource, SourceDigest: configrollout.SourceDigest(source), ProofDigest: proof.Digest()}
		d.rememberProof(proof.Digest(), deploymentSourceProof{database: proof, expires: time.Now().Add(5 * time.Minute)})
	}
	if selected.RootSource != "" && selected.RootSource != d.installed.RootSource {
		source, ok := d.enrollment.Target.RootSources[selected.RootSource]
		if !ok {
			return configrollout.SignedCommand{}, configrollout.ErrUnsupported
		}
		root, err := d.rootSource(selected.RootSource)
		if err != nil {
			return configrollout.SignedCommand{}, err
		}
		defer crypto.Zero(root)
		// Pure sealing is safe before approval. Only the service's final exact-
		// MFA transaction can persist this encrypted second wrapper.
		epoch, err := d.keyring.VerifyRootKeyRotation(ctx, root)
		var wrapper *crypto.WrappedKey
		if errors.Is(err, crypto.ErrNotDualWrapped) {
			sealed, prepareErr := d.keyring.PrepareRootKeyRotation(ctx, root)
			if prepareErr != nil {
				return configrollout.SignedCommand{}, configrollout.ErrUnsupported
			}
			wrapper, epoch, err = &sealed, sealed.RootKeyEpoch, nil
		}
		if err != nil || epoch < 2 {
			return configrollout.SignedCommand{}, configrollout.ErrUnsupported
		}
		var nonce [32]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return configrollout.SignedCommand{}, configrollout.ErrUnavailable
		}
		fingerprint := sha256.Sum256(root)
		evidence := sha256.New()
		_, _ = evidence.Write(nonce[:])
		_, _ = evidence.Write(fingerprint[:])
		if wrapper != nil {
			_, _ = evidence.Write(wrapper.Blob)
		}
		digest := hex.EncodeToString(evidence.Sum(nil))
		changes.Root = &configrollout.SourceProof{Alias: selected.RootSource, SourceDigest: configrollout.SourceDigest(source), ProofDigest: digest, RootEpoch: int64(epoch)}
		d.rememberProof(digest, deploymentSourceProof{root: fingerprint, epoch: epoch, wrapper: wrapper, expires: time.Now().Add(5 * time.Minute)})
	}
	if changes.Database == nil && changes.Root == nil {
		return configrollout.SignedCommand{}, configrollout.ErrInvalid
	}
	return d.sign(ctx, configrollout.Command{EnrollmentID: d.enrollment.ID, Sequence: sequence, Action: configrollout.ActionPrepare, Intent: intent, Bootstrap: changes})
}

// RootPreparation returns the exact encrypted candidate bound by the signed
// prepare proof. It performs no persistence or hierarchy changes. After submit
// reproof, the coordinator holds rotation exclusion while atomically consuming
// MFA, authorizing root rotation and writing this wrapper with the target.
func (d *bootstrapDeployment) RootPreparation(ctx context.Context, prepared configrollout.SignedCommand) (*crypto.WrappedKey, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !d.validSigned(prepared) || prepared.Command.Action != configrollout.ActionPrepare {
		return nil, configrollout.ErrInvalid
	}
	if prepared.Command.Bootstrap == nil || prepared.Command.Bootstrap.Root == nil {
		return nil, nil
	}
	source := prepared.Command.Bootstrap.Root
	d.mu.Lock()
	proof, ok := d.proofs[source.ProofDigest]
	d.mu.Unlock()
	if !ok || !time.Now().Before(proof.expires) || int64(proof.epoch) != source.RootEpoch {
		return nil, service.ErrDeploymentPreparationExpired
	}
	if proof.wrapper == nil {
		return nil, nil
	}
	wrapper := *proof.wrapper
	wrapper.Blob = bytes.Clone(proof.wrapper.Blob)
	return &wrapper, nil
}

func (d *bootstrapDeployment) DecisionCommand(ctx context.Context, prepared configrollout.SignedCommand, action configrollout.Action, sequence uint64, planDigest string, ack *configrollout.ApplicationAcknowledgement) (configrollout.SignedCommand, error) {
	if !d.validSigned(prepared) || sequence <= prepared.Command.Sequence {
		return configrollout.SignedCommand{}, configrollout.ErrInvalid
	}
	if prepared.Command.Action != configrollout.ActionPrepare && prepared.Command.PlanDigest != planDigest {
		return configrollout.SignedCommand{}, configrollout.ErrInvalid
	}
	switch action {
	case configrollout.ActionSubmit:
		if prepared.Command.Action != configrollout.ActionPrepare || ack != nil {
			return configrollout.SignedCommand{}, configrollout.ErrInvalid
		}
		if err := d.validatePreparedSources(ctx, prepared.Command.Bootstrap); err != nil {
			return configrollout.SignedCommand{}, err
		}
	case configrollout.ActionObserve:
		if ack != nil && (ack.Intent != prepared.Command.Intent || ack.PlanDigest != planDigest || string(ack.DeploymentUID) != d.identity.DeploymentUID) {
			return configrollout.SignedCommand{}, configrollout.ErrInvalid
		}
	case configrollout.ActionRestore:
		if ack != nil {
			return configrollout.SignedCommand{}, configrollout.ErrInvalid
		}
	default:
		return configrollout.SignedCommand{}, configrollout.ErrInvalid
	}
	return d.sign(ctx, configrollout.Command{EnrollmentID: d.enrollment.ID, Sequence: sequence, Action: action, Intent: prepared.Command.Intent, PlanDigest: planDigest, Acknowledgement: ack})
}

func (d *bootstrapDeployment) RenewCommand(ctx context.Context, committed configrollout.SignedCommand, sequence uint64) (configrollout.SignedCommand, error) {
	if !d.validSigned(committed) || sequence <= committed.Command.Sequence {
		return configrollout.SignedCommand{}, configrollout.ErrInvalid
	}
	switch committed.Command.Action {
	case configrollout.ActionSubmit, configrollout.ActionObserve, configrollout.ActionRestore:
	default:
		return configrollout.SignedCommand{}, configrollout.ErrInvalid
	}
	command := committed.Command
	command.Sequence = sequence
	// The durable decision already passed exact MFA and source proof. Preserve
	// every decision field; process-local preparation caches are not authority.
	return d.sign(ctx, command)
}

func (d *bootstrapDeployment) Send(ctx context.Context, signed configrollout.SignedCommand) error {
	if !d.validSigned(signed) {
		return configrollout.ErrInvalid
	}
	return d.mailbox.Send(ctx, signed)
}

func (d *bootstrapDeployment) Response(ctx context.Context, signed configrollout.SignedCommand) (configrollout.Response, error) {
	if !d.validSigned(signed) {
		return configrollout.Response{}, configrollout.ErrInvalid
	}
	return d.mailbox.Response(ctx, signed)
}

func (d *bootstrapDeployment) VerifyInstalled(ctx context.Context, bundle *runtimeconfig.Bundle) error {
	if bundle == nil {
		return service.ErrDeploymentSourcesPending
	}
	selected := bundle.BootstrapSources()
	if selected.Version == 0 {
		return nil
	}
	return d.verifySelections(ctx, selected)
}

func (d *bootstrapDeployment) verifySelections(ctx context.Context, expected config.ManagedBootstrapSources) error {
	selected, stamp, err := d.readSelection()
	if err != nil || selected != d.installed || stamp != d.identity.TemplateStamp {
		return service.ErrDeploymentSourcesPending
	}
	if expected.DatabaseSource != "" {
		if selected.DatabaseSource != expected.DatabaseSource {
			return service.ErrDeploymentSourcesPending
		}
		dsn, err := d.databaseSource(expected.DatabaseSource)
		if err != nil || dsn != strings.TrimSpace(d.cfg.Store.DSN) {
			return service.ErrDeploymentSourcesPending
		}
		if _, err := d.db.VerifyPostgresSource(ctx, dsn); err != nil {
			return service.ErrDeploymentSourcesPending
		}
	}
	if expected.RootSource != "" {
		if selected.RootSource != expected.RootSource {
			return service.ErrDeploymentSourcesPending
		}
		root, err := d.rootSource(expected.RootSource)
		if err != nil {
			return service.ErrDeploymentSourcesPending
		}
		defer crypto.Zero(root)
		current, err := d.currentRoot()
		if err != nil {
			return service.ErrDeploymentSourcesPending
		}
		defer crypto.Zero(current)
		if subtle.ConstantTimeCompare(root, current) != 1 {
			return service.ErrDeploymentSourcesPending
		}
		if err := crypto.VerifyExistingHierarchy(ctx, &keyring.Store{DB: d.db}, bytes.Clone(root)); err != nil {
			return service.ErrDeploymentSourcesPending
		}
	}
	return nil
}

func (d *bootstrapDeployment) sign(ctx context.Context, command configrollout.Command) (configrollout.SignedCommand, error) {
	command.IssuedAt = time.Now().UTC()
	command.ExpiresAt = command.IssuedAt.Add(5 * time.Minute)
	// A metadata roundtrip detaches nested input pointers before signing and
	// returning an immutable command for the service's durable transaction.
	raw, err := json.Marshal(command)
	var detached configrollout.Command
	if err != nil || json.Unmarshal(raw, &detached) != nil {
		return configrollout.SignedCommand{}, configrollout.ErrInvalid
	}
	signed, err := configrollout.SignCommand(ctx, d.signer, detached)
	if err != nil || !d.validSigned(signed) {
		return configrollout.SignedCommand{}, configrollout.ErrInvalid
	}
	return signed, nil
}

func (d *bootstrapDeployment) validSigned(signed configrollout.SignedCommand) bool {
	public, ok := d.signer.Public().(ed25519.PublicKey)
	return ok && configrollout.VerifySignedCommand(signed, d.enrollment, public)
}

func (d *bootstrapDeployment) rememberProof(digest string, proof deploymentSourceProof) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	for key, item := range d.proofs {
		if !now.Before(item.expires) {
			delete(d.proofs, key)
		}
	}
	if len(d.proofs) >= 64 {
		var oldest string
		var expiry time.Time
		for key, item := range d.proofs {
			if oldest == "" || item.expires.Before(expiry) {
				oldest, expiry = key, item.expires
			}
		}
		delete(d.proofs, oldest)
	}
	d.proofs[digest] = proof
}

func (d *bootstrapDeployment) validatePreparedSources(ctx context.Context, changes *configrollout.BootstrapChanges) error {
	if changes == nil {
		return configrollout.ErrInvalid
	}
	for _, source := range []*configrollout.SourceProof{changes.Database, changes.Root} {
		if source == nil {
			continue
		}
		d.mu.Lock()
		proof, ok := d.proofs[source.ProofDigest]
		d.mu.Unlock()
		if !ok || !time.Now().Before(proof.expires) {
			return service.ErrDeploymentPreparationExpired
		}
		if source == changes.Database {
			dsn, err := d.databaseSource(source.Alias)
			if err != nil || proof.database == nil {
				return configrollout.ErrUnavailable
			}
			if err := proof.database.ValidateFor(ctx, d.db, dsn, time.Now()); err != nil {
				return service.ErrDeploymentPreparationExpired
			}
		} else {
			root, err := d.rootSource(source.Alias)
			if err != nil {
				return configrollout.ErrUnavailable
			}
			fingerprint := sha256.Sum256(root)
			var epoch uint32
			var verifyErr error
			if proof.wrapper == nil {
				epoch, verifyErr = d.keyring.VerifyRootKeyRotation(ctx, root)
			} else {
				fresh, err := d.keyring.PrepareRootKeyRotation(ctx, root)
				epoch, verifyErr = fresh.RootKeyEpoch, err
				if err == nil && fresh.Version != proof.wrapper.Version {
					verifyErr = crypto.ErrStaleMaster
				}
				crypto.Zero(fresh.Blob)
			}
			crypto.Zero(root)
			if verifyErr != nil || int64(epoch) != source.RootEpoch || proof.epoch != epoch || subtle.ConstantTimeCompare(fingerprint[:], proof.root[:]) != 1 {
				return service.ErrDeploymentPreparationExpired
			}
		}
	}
	return nil
}

func (d *bootstrapDeployment) databaseSource(alias string) (string, error) {
	if _, ok := d.enrollment.Target.DatabaseSources[alias]; !ok || !config.ValidManagedNodeID(alias) {
		return "", configrollout.ErrUnsupported
	}
	raw, err := readDeploymentFile(filepath.Join(d.sourcesDirectory, "database", alias, "dsn"), false)
	if err != nil {
		return "", configrollout.ErrUnavailable
	}
	defer crypto.Zero(raw)
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "", configrollout.ErrUnavailable
	}
	return value, nil
}

func (d *bootstrapDeployment) rootSource(alias string) ([]byte, error) {
	if _, ok := d.enrollment.Target.RootSources[alias]; !ok || !config.ValidManagedNodeID(alias) {
		return nil, configrollout.ErrUnsupported
	}
	raw, err := readDeploymentFile(filepath.Join(d.sourcesDirectory, "root", alias, "root-key"), true)
	if err != nil {
		return nil, configrollout.ErrUnavailable
	}
	defer crypto.Zero(raw)
	root, err := crypto.ReadRootKey("", string(raw))
	if err != nil {
		return nil, configrollout.ErrUnavailable
	}
	return root, nil
}

func (d *bootstrapDeployment) currentRoot() ([]byte, error) {
	if d.cfg.RootKeyFromEnv {
		return crypto.ReadRootKey("", os.Getenv("HIKYO_ROOT_KEY"))
	}
	path := d.cfg.RootKeyFile
	if path == "" && d.cfg.Dev {
		path = devRootKeyPath(d.cfg)
	}
	raw, err := readDeploymentFile(path, true)
	if err != nil {
		return nil, configrollout.ErrUnavailable
	}
	defer crypto.Zero(raw)
	return crypto.ReadRootKey("", string(raw))
}

func (d *bootstrapDeployment) readSelection() (config.ManagedBootstrapSources, string, error) {
	values := make(map[string]string)
	for _, name := range []string{"stamp", "database-alias", "root-alias"} {
		raw, err := readDeploymentFile(filepath.Join(d.selectionDirectory, name), false)
		if err != nil {
			return config.ManagedBootstrapSources{}, "", configrollout.ErrUnavailable
		}
		values[name] = strings.TrimSpace(string(raw))
	}
	if stamp := values["stamp"]; stamp != "" {
		decoded, err := hex.DecodeString(stamp)
		if err != nil || len(decoded) != 32 {
			return config.ManagedBootstrapSources{}, "", configrollout.ErrUnavailable
		}
	}
	selected := config.ManagedBootstrapSources{DatabaseSource: values["database-alias"], RootSource: values["root-alias"]}
	if selected.DatabaseSource != "" || selected.RootSource != "" {
		selected.Version = 1
	}
	if selected.DatabaseSource != "" {
		if _, ok := d.enrollment.Target.DatabaseSources[selected.DatabaseSource]; !ok {
			return config.ManagedBootstrapSources{}, "", configrollout.ErrUnavailable
		}
	}
	if selected.RootSource != "" {
		if _, ok := d.enrollment.Target.RootSources[selected.RootSource]; !ok {
			return config.ManagedBootstrapSources{}, "", configrollout.ErrUnavailable
		}
	}
	return selected, values["stamp"], nil
}

func readDeploymentSigner(path string) (stdcrypto.Signer, error) {
	raw, err := readDeploymentFile(path, true)
	if err != nil {
		return nil, configrollout.ErrUnavailable
	}
	defer crypto.Zero(raw)
	block, trailing := pem.Decode(raw)
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("-----BEGIN PRIVATE KEY-----")) || block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(trailing)) != 0 || len(block.Headers) != 0 {
		return nil, configrollout.ErrUnavailable
	}
	defer crypto.Zero(block.Bytes)
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, configrollout.ErrUnavailable
	}
	private, ok := key.(ed25519.PrivateKey)
	if !ok || len(private) != ed25519.PrivateKeySize {
		return nil, configrollout.ErrUnavailable
	}
	return private, nil
}

func readDeploymentFile(path string, private bool) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
		return nil, configrollout.ErrUnavailable
	}
	if private && info.Mode().Perm() != 0400 && info.Mode().Perm() != 0600 {
		return nil, configrollout.ErrUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, configrollout.ErrUnavailable
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
		return nil, configrollout.ErrUnavailable
	}
	if private && info.Mode().Perm() != 0400 && info.Mode().Perm() != 0600 {
		return nil, configrollout.ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(raw) > 65536 {
		crypto.Zero(raw)
		return nil, configrollout.ErrUnavailable
	}
	return raw, nil
}
