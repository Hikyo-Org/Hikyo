package service

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// SelfConfig connects an instance's ordinary encrypted project revisions to
// its runtime consumers. Seed is lazy: an adopted instance never reads or
// validates old process settings. Remote instances own separate services and
// datastores; this object coordinates only replicas of its local owner.
type SelfConfig struct {
	DB           *store.DB
	Keyring      *crypto.Keyring
	Auth         *Auth
	Budget       *Budget
	NodeID       string
	HAMode       bool
	Now          func() time.Time
	Seed         func() (map[string]string, error)
	SeedNode     func() (map[string]string, error)
	Installer    runtimeconfig.RuntimeInstaller
	Deployment   BootstrapDeployment
	active       atomic.Pointer[selfConfigActive]
	originReview atomic.Pointer[selfConfigOriginReview]
	repairOrigin atomic.Pointer[selfConfigRepairOrigin]
	// installed retains origin evidence when a failed target fences Capture.
	// It grants no runtime authority and is never read by request admission.
	installed    atomic.Pointer[selfConfigActive]
	prepared     *selfConfigPrepared
	runtimeMu    sync.Mutex
	seedMu       sync.Mutex
	seed         *selfConfigSeed
	mailOnce     sync.Once
	mailOutcomes chan selfConfigMailOutcome
}

func (s *SelfConfig) now() time.Time { return nowOr(s.Now) }
