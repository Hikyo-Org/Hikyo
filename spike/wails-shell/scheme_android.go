//go:build android && !stockhost

package main

// Android serves https only, and a wildcard subdomain keeps one origin per connection.
const pageScheme = "https"

func newHost() string { return randomLabel() + ".wails.localhost" }

const windowHeaderSent = true
