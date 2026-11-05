//go:build android && probe

package main

import (
	"net/http"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func secureSet(k, v string) error              { return application.Android.SecureSet(k, v) }
func secureGet(k string) (string, bool, error) { return application.Android.SecureGet(k) }
func secureDelete(k string) error              { return application.Android.SecureDelete(k) }

// Safari has no counterpart: the only hop is the browser the Intent picks.
func hopOpen(_, u string) error { return openURL(u) }
func hopDone(string)            {}

func probeRoutes(func(string, bool, http.HandlerFunc)) {}
