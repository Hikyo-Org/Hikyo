//go:build !isorig

package main

import "net/http"

var rigCSP = false

func hostFor(session) string { return newHost() }

func logSignals(*http.Request) {}
