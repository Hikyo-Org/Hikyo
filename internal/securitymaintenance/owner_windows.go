package securitymaintenance

import "os"

// Automatic deployment authority on Windows has no qualified executor. Refuse
// private state rather than treating POSIX permission bits as an ACL check.
func ownedByCurrentUser(os.FileInfo) bool { return false }
