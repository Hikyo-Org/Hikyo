//go:build android && !stockhost

package main

import "fmt"

// Android serves https only, and a wildcard subdomain keeps one origin per connection.
const pageScheme = "https"

func connHost(i int) string { return fmt.Sprintf("conn-%d.wails.localhost", i) }

const windowHeaderSent = true
