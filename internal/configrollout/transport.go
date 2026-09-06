package configrollout

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
)

const commandKey = "command.json"
const responseKey = "response.json"
const journalKey = "journal.json"
const commandDomain = "hikyo.configuration-rollout.command.v1\x00"

type Action string

const (
	ActionPrepare Action = "prepare"
	ActionSubmit  Action = "submit"
	ActionObserve Action = "observe"
	ActionRestore Action = "restore"
)

// Enrollment is operator-installed custody. Enabling the provider requires the
// already created Deployment UID and the admitted owner's recovery incarnation.
// The authority key is separate from every Hikyo root, DEK and project key.
type Enrollment struct {
	ID                string    `json:"id"`
	OwnerInstanceID   string    `json:"owner_instance_id"`
	Incarnation       string    `json:"incarnation"`
	Target            Target    `json:"target"`
	CommandSecret     string    `json:"command_secret"`
	CommandSecretUID  types.UID `json:"command_secret_uid"`
	ResponseSecret    string    `json:"response_secret"`
	ResponseSecretUID types.UID `json:"response_secret_uid"`
	JournalSecret     string    `json:"journal_secret"`
	JournalSecretUID  types.UID `json:"journal_secret_uid"`
	LeaseName         string    `json:"lease_name"`
	LeaseUID          types.UID `json:"lease_uid"`
	ExecutorPod       string    `json:"executor_pod"`
}

// Command is emitted only after the coordinator durably authorizes its action.
// Prepare is read-only planning; submit requires the exact administrator-MFA
// decision. Sequence is a durable per-enrollment counter. A prepare response's
// PlanDigest must be committed before signing submit.
type Command struct {
	EnrollmentID    string                      `json:"enrollment_id"`
	Sequence        uint64                      `json:"sequence"`
	Action          Action                      `json:"action"`
	Intent          Intent                      `json:"intent"`
	PlanDigest      string                      `json:"plan_digest,omitempty"`
	Changes         []Change                    `json:"changes,omitempty"`
	Bootstrap       *BootstrapChanges           `json:"bootstrap,omitempty"`
	Acknowledgement *ApplicationAcknowledgement `json:"acknowledgement,omitempty"`
	IssuedAt        time.Time                   `json:"issued_at"`
	ExpiresAt       time.Time                   `json:"expires_at"`
}

type SignedCommand struct {
	Command   Command `json:"command"`
	Signature []byte  `json:"signature"`
}

// Response contains no configuration values, source contents or credentials.
type Response struct {
	EnrollmentID  string           `json:"enrollment_id"`
	Sequence      uint64           `json:"sequence"`
	CommandDigest string           `json:"command_digest"`
	PlanDigest    string           `json:"plan_digest,omitempty"`
	TemplateStamp string           `json:"template_stamp,omitempty"`
	Resources     ResourceVersions `json:"resources"`
	Receipt       *Receipt         `json:"receipt,omitempty"`
	Outcome       string           `json:"outcome"`
}

func validEnrollment(e Enrollment) bool {
	if !safeName(e.ID) || !safeName(e.OwnerInstanceID) || !safeName(e.Incarnation) || !safeName(e.Target.StableNodeID) || e.CommandSecretUID == "" || e.ResponseSecretUID == "" || e.JournalSecretUID == "" || e.LeaseUID == "" || e.Target.DeploymentUID == "" || len(validation.IsDNS1123Label(e.Target.Namespace)) != 0 {
		return false
	}
	seen := map[string]bool{}
	for _, name := range []string{e.Target.ConfigSecret, e.Target.RollbackSecret, e.Target.RequestSecret, e.Target.ReceiptSecret, e.CommandSecret, e.ResponseSecret, e.JournalSecret} {
		if len(validation.IsDNS1123Subdomain(name)) != 0 || seen[name] {
			return false
		}
		seen[name] = true
	}
	return len(validation.IsDNS1123Subdomain(e.LeaseName)) == 0 && len(validation.IsDNS1123Subdomain(e.ExecutorPod)) == 0
}

// ParseEnrollment rejects unknown fields and trailing data in installed custody.
func ParseEnrollment(raw []byte) (Enrollment, error) {
	var enrollment Enrollment
	if decode(raw, &enrollment) != nil || !validEnrollment(enrollment) {
		return Enrollment{}, ErrInvalid
	}
	return cloneEnrollment(enrollment), nil
}

func validCommand(c Command, e Enrollment) bool {
	if c.EnrollmentID != e.ID || c.Sequence == 0 || !validIntent(c.Intent) || c.Intent.OwnerInstanceID != e.OwnerInstanceID || c.Intent.Incarnation != e.Incarnation || c.IssuedAt.IsZero() || !c.ExpiresAt.After(c.IssuedAt) || c.ExpiresAt.Sub(c.IssuedAt) > 10*time.Minute {
		return false
	}
	switch c.Action {
	case ActionPrepare:
		return c.PlanDigest == "" && (len(c.Changes) > 0 || c.Bootstrap != nil) && len(c.Changes) <= 32 && c.Acknowledgement == nil && (&Kubernetes{target: e.Target}).validBootstrap(c.Bootstrap)
	case ActionSubmit, ActionRestore:
		return validDigest(c.PlanDigest) && len(c.Changes) == 0 && c.Bootstrap == nil && c.Acknowledgement == nil
	case ActionObserve:
		return validDigest(c.PlanDigest) && len(c.Changes) == 0 && c.Bootstrap == nil && (c.Acknowledgement == nil || c.Acknowledgement.Intent == c.Intent && c.Acknowledgement.PlanDigest == c.PlanDigest && c.Acknowledgement.DeploymentUID == e.Target.DeploymentUID)
	}
	return false
}

// SignCommand uses the standard Ed25519 signer. It deliberately does not retain
// private key material or claim that signing replaces the coordinator's MFA.
func SignCommand(ctx context.Context, signer crypto.Signer, command Command) (SignedCommand, error) {
	if err := ctx.Err(); err != nil {
		return SignedCommand{}, err
	}
	if signer == nil {
		return SignedCommand{}, ErrInvalid
	}
	if key, ok := signer.Public().(ed25519.PublicKey); !ok || len(key) != ed25519.PublicKeySize {
		return SignedCommand{}, ErrInvalid
	}
	raw, err := json.Marshal(command)
	if err != nil || len(raw) > maxRecordBytes {
		return SignedCommand{}, ErrInvalid
	}
	sig, err := signer.Sign(rand.Reader, append([]byte(commandDomain), raw...), crypto.Hash(0))
	if err != nil {
		return SignedCommand{}, ErrUnavailable
	}
	return SignedCommand{Command: command, Signature: sig}, nil
}

func verifyCommand(signed SignedCommand, e Enrollment, public ed25519.PublicKey) bool {
	if len(public) != ed25519.PublicKeySize || !validCommand(signed.Command, e) {
		return false
	}
	raw, err := json.Marshal(signed.Command)
	return err == nil && len(raw) <= maxRecordBytes && ed25519.Verify(public, append([]byte(commandDomain), raw...), signed.Signature)
}

// VerifySignedCommand authenticates a persisted command against enrollment.
// TTL is enforced at first acceptance; fresh submits need fresh source proofs.
func VerifySignedCommand(signed SignedCommand, e Enrollment, public ed25519.PublicKey) bool {
	return verifyCommand(signed, e, public)
}

// Mailbox is the server's entire Kubernetes authority: write one precreated
// command Secret and read one response Secret. It cannot read rollout plans,
// database credentials, root keys or ordinary HikyoSecret project deliveries.
type Mailbox struct {
	client     kubernetes.Interface
	enrollment Enrollment
}

func NewMailbox(client kubernetes.Interface, e Enrollment) (*Mailbox, error) {
	if client == nil || !validEnrollment(e) {
		return nil, ErrInvalid
	}
	return &Mailbox{client: client, enrollment: cloneEnrollment(e)}, nil
}

func (m *Mailbox) Send(ctx context.Context, signed SignedCommand) error {
	if !validCommand(signed.Command, m.enrollment) || len(signed.Signature) != ed25519.SignatureSize {
		return ErrInvalid
	}
	raw := mustJSON(signed)
	if len(raw) > maxRecordBytes {
		return ErrInvalid
	}
	s, err := m.client.CoreV1().Secrets(m.enrollment.Target.Namespace).Get(ctx, m.enrollment.CommandSecret, metav1.GetOptions{})
	if err != nil {
		return apiError(err)
	}
	if s.UID != m.enrollment.CommandSecretUID || s.Immutable != nil && *s.Immutable {
		return ErrConflict
	}
	if bytes.Equal(s.Data[commandKey], raw) {
		return nil
	}
	if len(s.Data[commandKey]) > 0 {
		var previous SignedCommand
		if decode(s.Data[commandKey], &previous) != nil || previous.Command.EnrollmentID != signed.Command.EnrollmentID || previous.Command.Sequence >= signed.Command.Sequence {
			return ErrConflict
		}
	}
	s.Data = map[string][]byte{commandKey: raw}
	_, err = m.client.CoreV1().Secrets(m.enrollment.Target.Namespace).Update(ctx, s, metav1.UpdateOptions{})
	if err != nil {
		return apiError(err)
	}
	return nil
}

func (m *Mailbox) Response(ctx context.Context, signed SignedCommand) (Response, error) {
	s, err := m.client.CoreV1().Secrets(m.enrollment.Target.Namespace).Get(ctx, m.enrollment.ResponseSecret, metav1.GetOptions{})
	if err != nil {
		return Response{}, apiError(err)
	}
	if s.UID != m.enrollment.ResponseSecretUID {
		return Response{}, ErrConflict
	}
	var response Response
	if decode(s.Data[responseKey], &response) != nil {
		return Response{}, ErrNotSubmitted
	}
	if response.EnrollmentID != signed.Command.EnrollmentID || response.Sequence != signed.Command.Sequence || response.CommandDigest != digest(signed) {
		return Response{}, ErrNotSubmitted
	}
	return response, nil
}

func fixedSecret(ctx context.Context, client kubernetes.Interface, namespace, name string, uid types.UID) (*corev1.Secret, error) {
	s, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, apiError(err)
	}
	if s.UID != uid || s.Immutable != nil && *s.Immutable {
		return nil, ErrConflict
	}
	return s, nil
}
