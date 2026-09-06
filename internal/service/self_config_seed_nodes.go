package service

import (
	"context"
	"encoding/json"
	"errors"
	"maps"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

type selfConfigNodeSeedEnvelope struct {
	Owner       string            `json:"owner"`
	Incarnation string            `json:"incarnation"`
	NodeID      string            `json:"node_id"`
	Values      map[string]string `json:"values"`
	OwnerValues map[string]string `json:"owner_values"`
}

func selfConfigNodeSeedAAD(nodeID string) crypto.InstanceFieldAAD {
	return crypto.InstanceFieldAAD{OwnerTable: "self_config_seed_inputs", OwnerRowID: nodeID, FieldTag: "inputs"}
}

func encodeSelfConfigNodeSeed(owner, incarnation, nodeID string, values, ownerValues map[string]string) ([]byte, error) {
	return json.Marshal(selfConfigNodeSeedEnvelope{Owner: owner, Incarnation: incarnation, NodeID: nodeID, Values: values, OwnerValues: ownerValues})
}

func (s *SelfConfig) attestNodeSeed(ctx context.Context, seed selfConfigSeed) error {
	if operation.IsNetwork(ctx) {
		return domain.ErrUnauthorized
	}
	raw, err := encodeSelfConfigNodeSeed(seed.owner, seed.incarnation, s.NodeID, seed.nodeValues, seed.values)
	if err != nil {
		return err
	}
	defer crypto.Zero(raw)
	fingerprint, err := s.Keyring.SelfConfigAdoptionToken(seed.owner, raw)
	if err != nil {
		return err
	}
	sealer := s.Keyring.ForInstance()
	ciphertext, err := sealer.SealField(selfConfigNodeSeedAAD(s.NodeID), raw)
	if err != nil {
		return err
	}
	at, err := s.runtimeTimestamp(ctx)
	if err != nil {
		return err
	}
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		proof, err := az.SelfConfigSeedAuthority(ctx)
		if err != nil {
			return err
		}
		if err := az.AssertActiveInstanceDEKVersion(ctx, int64(sealer.Version())); err != nil {
			return err
		}
		return r.SelfConfig().PutSeedInput(ctx, proof, store.SelfConfigSeedInput{NodeID: s.NodeID, OwnerInstanceID: seed.owner, Incarnation: seed.incarnation, Fingerprint: fingerprint, OwnerFingerprint: seed.ownerToken, Ciphertext: ciphertext, DEKVersion: int64(sealer.Version()), SchemaVersion: runtimeconfig.SchemaVersion, UpdatedAt: at})
	})
}

// prepareAdoptionSeed aggregates the exact admitted replicas' encrypted inputs.
// Owner settings must agree; node values remain addressed by their own node ID.
// Human reads require the existing MFA-protected adoption preview authority.
// A nil actor is only the closed host setup path, never a network authority.
func (s *SelfConfig) prepareAdoptionSeed(ctx context.Context, actor *Actor) (selfConfigSeed, error) {
	if actor == nil && operation.IsNetwork(ctx) {
		return selfConfigSeed{}, domain.ErrUnauthorized
	}
	if s.SeedNode == nil {
		return s.prepareSeed()
	}
	var seed selfConfigSeed
	var err error
	if actor == nil {
		// The host command is an importer, not a server. Its parser defaults and
		// environment must never overwrite the running node's startup attestation.
		seed.owner, seed.incarnation, err = s.DB.RecoveryIdentity()
		seed.hostSeedDiscovery = true
	} else {
		seed, err = s.prepareSeed()
	}
	if err != nil {
		return selfConfigSeed{}, err
	}
	at, err := s.runtimeTimestamp(ctx)
	if err != nil {
		return selfConfigSeed{}, err
	}
	var inputs []store.SelfConfigSeedInput
	err = tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		var proof authz.Proof
		var err error
		if actor == nil {
			proof, err = az.SelfConfigSeedAuthority(ctx)
		} else {
			_, proof, err = authorize(ctx, az, *actor, authz.OpSelfConfigPreview, domain.Scope{}, s.now())
		}
		if err != nil {
			return err
		}
		if actor == nil {
			inputs, err = r.SelfConfig().HostSeedInputs(ctx, proof, at)
		} else {
			inputs, err = r.SelfConfig().SeedInputs(ctx, proof, s.NodeID, at)
		}
		return err
	})
	if err != nil {
		if actor == nil && errors.Is(err, store.ErrSelfConfigSeedDisagreement) {
			return selfConfigSeed{}, errors.Join(err, errors.New("fresh server configuration is required; start the server, then retry admin create"))
		}
		return selfConfigSeed{}, err
	}
	nodes := make(map[string]map[string]string, len(inputs))
	for _, input := range inputs {
		if input.OwnerInstanceID != seed.owner || input.Incarnation != seed.incarnation || input.SchemaVersion != runtimeconfig.SchemaVersion {
			return selfConfigSeed{}, store.ErrSelfConfigSeedDisagreement
		}
		plain, err := s.Keyring.ForInstance().OpenField(selfConfigNodeSeedAAD(input.NodeID), input.Ciphertext)
		if err != nil {
			return selfConfigSeed{}, errors.New("self-configuration node seed cannot be decrypted")
		}
		var envelope selfConfigNodeSeedEnvelope
		decodeErr := json.Unmarshal(plain, &envelope)
		fingerprint, fingerprintErr := s.Keyring.SelfConfigAdoptionToken(seed.owner, plain)
		crypto.Zero(plain)
		if decodeErr != nil || fingerprintErr != nil || fingerprint != input.Fingerprint || envelope.Owner != seed.owner || envelope.Incarnation != seed.incarnation || envelope.NodeID != input.NodeID {
			return selfConfigSeed{}, store.ErrSelfConfigSeedDisagreement
		}
		if envelope.OwnerValues == nil {
			return selfConfigSeed{}, store.ErrSelfConfigSeedDisagreement
		}
		ownerToken, err := s.ownerSeedToken(seed.owner, envelope.OwnerValues)
		if err != nil || ownerToken != input.OwnerFingerprint {
			return selfConfigSeed{}, store.ErrSelfConfigSeedDisagreement
		}
		if actor == nil && seed.ownerToken == "" {
			seed.values = maps.Clone(envelope.OwnerValues)
			seed.ownerToken = ownerToken
		}
		if ownerToken != seed.ownerToken {
			return selfConfigSeed{}, store.ErrSelfConfigSeedDisagreement
		}
		nodes[input.NodeID] = envelope.Values
		seed.nodeReferences = append(seed.nodeReferences, store.SelfConfigSeedReference{NodeID: input.NodeID, Fingerprint: input.Fingerprint})
	}
	// This exact representation is also checked by the normal runtime parser.
	// It is a single secret project value, preserving normal publication,
	// encryption, snapshot retention and historical reauthentication behavior.
	encodedNodes, err := runtimeconfig.EncodeNodeOverrides(nodes)
	if err != nil {
		return selfConfigSeed{}, err
	}
	seed.values = maps.Clone(seed.values)
	seed.values["HIKYO_NODE_OVERRIDES"] = encodedNodes
	if _, err := runtimeconfig.Prepare(seed.values); err != nil {
		return selfConfigSeed{}, err
	}
	seed.token, err = s.ownerSeedToken(seed.owner, seed.values)
	return seed, err
}

// ownerSeedToken uses the same canonical envelope as prepareSeed. Owner values
// remain inside instance-DEK ciphertext until exact protected-project adoption.
func (s *SelfConfig) ownerSeedToken(owner string, values map[string]string) (string, error) {
	raw, err := json.Marshal(struct {
		Schema int
		Values map[string]string
	}{runtimeconfig.SchemaVersion, values})
	if err != nil {
		return "", err
	}
	defer crypto.Zero(raw)
	return s.Keyring.SelfConfigAdoptionToken(owner, raw)
}
