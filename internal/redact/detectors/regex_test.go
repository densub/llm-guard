package detectors

import "testing"

func TestRegexDetector_Builtin(t *testing.T) {
	cases := []struct {
		category string
		sample   string
	}{
		{"aws_access_key", "AKIAIOSFODNN7EXAMPLE"},
		{"aws_secret_key", `aws_secret_access_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`},
		{"gcp_api_key", "AIzaSyXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"},
		{"github_token", "ghp_1234567890abcdefghijklmnopqrstuvwxyz12"},
		{"gitlab_token", "glpat-XXXXXXXXXXXXXXXXXXXX"},
		{"slack_token", "xoxb-1234567890123"},
		{"stripe_key", "sk_live_1234567890abcdefghijklmnop"},
		{"openai_key", "sk-1234567890abcdefghijklmnop"},
		{"anthropic_key", "sk-ant-api03XXXXXXXXXXXXXXXXXXXXXXXX"},
		{"private_key_block", "-----BEGIN RSA PRIVATE KEY-----\nMIIBVQIBADANBgkqhkiG\n-----END RSA PRIVATE KEY-----"},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dGVzdHNpZ25hdHVyZQ"},
		{"generic_api_key_assignment", "api_key=abcd1234efgh5678"},
		{"email", "alice@example.com"},
	}

	for _, tc := range cases {
		t.Run(tc.category, func(t *testing.T) {
			d, err := NewRegexDetector([]string{tc.category}, nil)
			if err != nil {
				t.Fatalf("NewRegexDetector: %v", err)
			}
			text := "prefix " + tc.sample + " suffix"
			matches := d.Detect(text)
			if len(matches) == 0 {
				t.Fatalf("category %s: expected a match in %q, got none", tc.category, text)
			}
			found := false
			for _, m := range matches {
				if m.Category == tc.category && m.Value == tc.sample {
					found = true
				}
			}
			if !found {
				t.Errorf("category %s: expected match value %q, got %+v", tc.category, tc.sample, matches)
			}
		})
	}
}

func TestNewRegexDetector_UnknownCategory(t *testing.T) {
	if _, err := NewRegexDetector([]string{"not_a_real_category"}, nil); err == nil {
		t.Fatal("expected error for unknown category, got nil")
	}
}

func TestNewRegexDetector_CustomPattern(t *testing.T) {
	d, err := NewRegexDetector(nil, []CustomPattern{{Name: "internal_proj", Pattern: `PROJ-[0-9]{4,6}`}})
	if err != nil {
		t.Fatalf("NewRegexDetector: %v", err)
	}
	matches := d.Detect("see ticket PROJ-12345 for details")
	if len(matches) != 1 || matches[0].Value != "PROJ-12345" || matches[0].Category != "internal_proj" {
		t.Fatalf("unexpected matches: %+v", matches)
	}
}
