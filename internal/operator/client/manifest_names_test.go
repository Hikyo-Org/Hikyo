package client

import (
	"fmt"
	"strings"
	"testing"
)

func TestDeliveryManifestKeyNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys string
		want bool
	}{
		{"valid", `{"name":"_KEY_1","classification":"config","presence":"set","value":"private-payload"}`, true},
		{"maximum", fmt.Sprintf(`{"name":%q,"classification":"config","presence":"set","value":"private-payload"}`, strings.Repeat("A", 128)), true},
		{"empty", `{"name":"","classification":"config","presence":"set","value":"private-payload"}`, false},
		{"lowercase", `{"name":"path","classification":"config","presence":"set","value":"private-payload"}`, false},
		{"wildcard", `{"name":"LD_*","classification":"config","presence":"set","value":"private-payload"}`, false},
		{"too long", fmt.Sprintf(`{"name":%q,"classification":"config","presence":"set","value":"private-payload"}`, strings.Repeat("A", 129)), false},
		{"duplicate", `{"name":"KEY","classification":"config","presence":"set","value":"private-payload"},{"name":"KEY","classification":"secret","presence":"set"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := fmt.Sprintf(`{"current":false,"cursor":"cursor","change_token":"token","schema_revision":1,"pin_expired":false,"keys":[%s]}`, tc.keys)
			_, err := decodeDelivery([]byte(payload))
			if (err == nil) != tc.want {
				t.Fatalf("accepted=%v, want %v: %v", err == nil, tc.want, err)
			}
			if err != nil && strings.Contains(err.Error(), "private-payload") {
				t.Fatal("validation leaked value")
			}
		})
	}
}
