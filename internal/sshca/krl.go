package sshca

import (
	"encoding/binary"
	"errors"
	"slices"
	"time"

	"golang.org/x/crypto/ssh"
)

// OpenSSH Key Revocation List encoding (OpenSSH PROTOCOL.krl, format version
// 1). Only the certificate-serial section is produced: Hikyo revokes exactly
// the certificates it issued, by CA key and serial. The format is a container
// of big-endian integers and length-prefixed strings; no primitive lives here.
const (
	krlMagic              uint64 = 0x5353484b524c0a00 // "SSHKRL\n\0"
	krlFormatVersion      uint32 = 1
	krlSectionCerts       byte   = 1
	krlCertSectionSerials byte   = 0x20
)

// MaxKRLSerials bounds one CA's KRL. It is far above any realistic revocation
// volume inside one max_ttl window; exceeding it is refused loudly by the
// service rather than truncated, because a truncated KRL silently un-revokes.
const MaxKRLSerials = 65536

// ErrKRLTooLarge reports a KRL over MaxKRLSerials.
var ErrKRLTooLarge = errors.New("sshca: key revocation list exceeds its bound")

// KRLSection revokes serials issued by one CA key.
type KRLSection struct {
	CAKey   ssh.PublicKey
	Serials []uint64
}

// KRL is one revocation list.
type KRL struct {
	Version     uint64
	GeneratedAt time.Time
	Comment     string
	Sections    []KRLSection
}

type krlWriter struct{ buf []byte }

func (w *krlWriter) u8(v byte)    { w.buf = append(w.buf, v) }
func (w *krlWriter) u32(v uint32) { w.buf = binary.BigEndian.AppendUint32(w.buf, v) }
func (w *krlWriter) u64(v uint64) { w.buf = binary.BigEndian.AppendUint64(w.buf, v) }
func (w *krlWriter) str(b []byte) {
	w.u32(uint32(len(b)))
	w.buf = append(w.buf, b...)
}

// EncodeKRL serializes a KRL. Serials are de-duplicated and sorted; zero
// serials (unrevocable by the format) and empty sections are dropped.
func EncodeKRL(k KRL) ([]byte, error) {
	total := 0
	w := &krlWriter{}
	w.u64(krlMagic)
	w.u32(krlFormatVersion)
	w.u64(k.Version)
	generated := k.GeneratedAt.Unix()
	if generated < 0 {
		generated = 0
	}
	w.u64(uint64(generated))
	w.u64(0)   // flags
	w.str(nil) // reserved
	w.str([]byte(k.Comment))
	for _, section := range k.Sections {
		if section.CAKey == nil {
			return nil, errors.New("sshca: KRL section needs its CA key")
		}
		serials := make([]uint64, 0, len(section.Serials))
		for _, s := range section.Serials {
			if s != 0 {
				serials = append(serials, s)
			}
		}
		slices.Sort(serials)
		serials = slices.Compact(serials)
		if len(serials) == 0 {
			continue
		}
		total += len(serials)
		if total > MaxKRLSerials {
			return nil, ErrKRLTooLarge
		}
		sub := &krlWriter{}
		for _, s := range serials {
			sub.u64(s)
		}
		body := &krlWriter{}
		body.str(section.CAKey.Marshal())
		body.str(nil) // reserved
		body.u8(krlCertSectionSerials)
		body.str(sub.buf)
		w.u8(krlSectionCerts)
		w.str(body.buf)
	}
	return w.buf, nil
}
