// tests/unit/webhook_lan_test.go — exercises the LAN-only URL validator
// applied to webhook destinations. The validator is unexported in the
// webhooks package; we cover it through a test-only export shim
// (webhooks.ValidateLANURLForTest).
package unit_test

import (
	"testing"

	"portal/internal/app/webhooks"
)

func TestWebhookLANValidator_AcceptsLoopback(t *testing.T) {
	cases := []string{
		"http://localhost/hook",
		"http://127.0.0.1:8080/hook",
		"http://[::1]/hook",
	}
	for _, u := range cases {
		if err := webhooks.ValidateLANURLForTest(u, nil); err != nil {
			t.Errorf("loopback %s should be accepted: %v", u, err)
		}
	}
}

func TestWebhookLANValidator_AcceptsRFC1918(t *testing.T) {
	cases := []string{
		"http://10.0.0.5/hook",
		"http://172.16.0.1/hook",
		"http://192.168.1.42/hook",
		"http://100.64.1.1/hook", // RFC6598 carrier-grade NAT
	}
	for _, u := range cases {
		if err := webhooks.ValidateLANURLForTest(u, nil); err != nil {
			t.Errorf("private %s should be accepted: %v", u, err)
		}
	}
}

func TestWebhookLANValidator_RejectsPublicIP(t *testing.T) {
	cases := []string{
		"http://8.8.8.8/exfil",
		"http://1.1.1.1/hook",
		"https://93.184.216.34/path", // example.com
	}
	for _, u := range cases {
		if err := webhooks.ValidateLANURLForTest(u, nil); err == nil {
			t.Errorf("public IP %s should be rejected", u)
		}
	}
}

func TestWebhookLANValidator_RejectsRandomHostname(t *testing.T) {
	if err := webhooks.ValidateLANURLForTest("http://exfil.example.com/hook", nil); err == nil {
		t.Error("public hostname should be rejected when not on allowlist")
	}
}

func TestWebhookLANValidator_AcceptsAllowlistedHostname(t *testing.T) {
	allow := []string{"internal-erp.corp", "files.lan"}
	if err := webhooks.ValidateLANURLForTest("http://internal-erp.corp/hook", allow); err != nil {
		t.Errorf("allowlisted hostname should be accepted: %v", err)
	}
	if err := webhooks.ValidateLANURLForTest("http://Files.LAN/path", allow); err != nil {
		t.Errorf("allowlist comparison must be case-insensitive: %v", err)
	}
}

func TestWebhookLANValidator_RejectsNonHTTP(t *testing.T) {
	cases := []string{
		"ftp://10.0.0.1/exfil",
		"file:///etc/passwd",
		"javascript:alert(1)",
	}
	for _, u := range cases {
		if err := webhooks.ValidateLANURLForTest(u, nil); err == nil {
			t.Errorf("non-http(s) %s should be rejected", u)
		}
	}
}
