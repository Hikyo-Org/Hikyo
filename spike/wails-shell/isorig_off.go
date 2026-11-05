//go:build !isorig

package main

import "net/http"

var rigCSP = false

func logSignals(*http.Request) {}
