// Package badline is a fixture whose production files claim, through //line
// directives, to be a _test.go file or the exempt reencrypt.go. Every
// diagnostic must still be judged by the file on disk.
//
//line claimed_test.go:1
package badline

import "github.com/Hikyo-Org/hikyo/internal/store/pggen"

var _ = pggen.New
