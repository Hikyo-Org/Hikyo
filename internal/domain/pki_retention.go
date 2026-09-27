package domain

// ErrPKICertificateRetention retains issuance and revocation evidence, including
// expired and failed records. Deleting a parent is not a PKI retention policy.
var ErrPKICertificateRetention error = pkiCertificateRetentionError{}

type pkiCertificateRetentionError struct{}

func (pkiCertificateRetentionError) Error() string {
	return "PKI certificate history prevents deletion"
}
func (pkiCertificateRetentionError) Unwrap() error { return ErrConflict }
func (pkiCertificateRetentionError) SafeDetail() string {
	return "PKI certificate history retains this resource. Revoke certificates and service-account credentials instead of deleting their parent."
}
