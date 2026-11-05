//go:build !android

package main

import "fmt"

// pageScheme and connHost name the origin a window loads: wails://conn-N on desktop and iOS.
const pageScheme = "wails"

func connHost(i int) string { return fmt.Sprintf("conn-%d", i) }

const windowHeaderSent = true
