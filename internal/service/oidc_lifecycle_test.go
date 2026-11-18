package service

import (
	"net/http"
	"testing"

	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
)

// The published Standard Webhooks / Svix test vector. The library has no
// injectable clock, so the timestamp window is skipped; the signature math is
// what the vector pins.
func TestLifecycleSvixVector(t *testing.T) {
	wh, err := standardwebhooks.NewWebhook("whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"test": 2432232314}`)
	svix := http.Header{}
	svix.Set("Svix-Id", "msg_p5jXN8AQM9LWM0D4loKWxJek")
	svix.Set("Svix-Timestamp", "1614265330")
	svix.Set("Svix-Signature", "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE=")

	if err := wh.VerifyIgnoringTimestamp(body, lifecycleHeaders(LifecycleProfileClerk, svix)); err != nil {
		t.Fatalf("clerk profile did not map svix-* headers: %v", err)
	}
	if err := wh.VerifyIgnoringTimestamp(body, svix); err == nil {
		t.Fatal("svix-* headers verified without the profile mapping")
	}
	if err := wh.VerifyIgnoringTimestamp([]byte(`{"test": 2432232315}`), lifecycleHeaders(LifecycleProfileClerk, svix)); err == nil {
		t.Fatal("a tampered body verified")
	}

	// A standard header already present wins over its svix-* twin.
	mixed := svix.Clone()
	mixed.Set("Webhook-Signature", "v1,AAAA")
	if got := lifecycleHeaders(LifecycleProfileClerk, mixed).Get("Webhook-Signature"); got != "v1,AAAA" {
		t.Fatalf("a standard header was overwritten: %q", got)
	}
}
