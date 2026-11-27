package lint

import "github.com/Hikyo-Org/hikyo/internal/modroot"

// moduleRoot is the module root for the tests, which always run inside the
// module.
func moduleRoot() string {
	root, err := modroot.Find()
	if err != nil {
		panic(err)
	}
	return root
}
