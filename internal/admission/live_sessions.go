package admission

import (
	"crypto/sha256"
	"time"
)

// liveSession is what a resolution proved: which session a verifier belongs to,
// so its request budget can be charged before the next read.
type liveSession struct {
	id   string
	seen time.Time
}

// KnownLiveSession returns the session this node last resolved the verifier to.
func (l *Limiter) KnownLiveSession(verifier []byte) (string, bool) {
	l.lifecycle.RLock()
	defer l.lifecycle.RUnlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	session, known := l.liveSessions[sha256.Sum256(verifier)]
	return session.id, known
}

// NoteLiveSession remembers a session a resolution just proved live. Losing an
// entry is always safe: its next presentation is charged once and noted again.
func (l *Limiter) NoteLiveSession(verifier []byte, sessionID string) {
	key := subjectKey(sha256.Sum256(verifier))
	l.lifecycle.RLock()
	defer l.lifecycle.RUnlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, known := l.liveSessions[key]; !known && len(l.liveSessions) >= MaxTrackedSubjects {
		// shortcut: linear scan under the limiter lock once this many sessions were seen since boot; use an LRU if that becomes routine.
		var oldestKey subjectKey
		var oldest time.Time
		for candidate, session := range l.liveSessions {
			if oldest.IsZero() || session.seen.Before(oldest) {
				oldestKey, oldest = candidate, session.seen
			}
		}
		delete(l.liveSessions, oldestKey)
	}
	l.liveSessions[key] = liveSession{id: sessionID, seen: l.now()}
}

// ForgetSession charges the verifier's next presentation like any stranger's.
func (l *Limiter) ForgetSession(verifier []byte) {
	l.lifecycle.RLock()
	defer l.lifecycle.RUnlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.liveSessions, sha256.Sum256(verifier))
}
