package token

import (
	"testing"
	"time"
)

func TestIssuer_IssueParseRoundtrip(t *testing.T) {
	iss := NewIssuer("secret", time.Hour)

	tok, err := iss.Issue(42)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	claims, err := iss.Parse(tok)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.UserID != 42 {
		t.Errorf("UserID = %d, want 42", claims.UserID)
	}
	if claims.Subject != "42" {
		t.Errorf("Subject = %q, want \"42\"", claims.Subject)
	}
}

func TestIssuer_ParseRejectsTamperedToken(t *testing.T) {
	iss := NewIssuer("secret", time.Hour)
	tok, _ := iss.Issue(1)

	// Flip the final character to corrupt the signature.
	tampered := tok[:len(tok)-1]
	if tok[len(tok)-1] == 'a' {
		tampered += "b"
	} else {
		tampered += "a"
	}

	if _, err := iss.Parse(tampered); err == nil {
		t.Error("expected error for tampered token, got nil")
	}
}

func TestIssuer_ParseRejectsWrongSecret(t *testing.T) {
	signer := NewIssuer("secret-a", time.Hour)
	verifier := NewIssuer("secret-b", time.Hour)

	tok, _ := signer.Issue(1)
	if _, err := verifier.Parse(tok); err == nil {
		t.Error("expected error for token signed with a different secret, got nil")
	}
}

func TestIssuer_ParseRejectsExpiredToken(t *testing.T) {
	iss := NewIssuer("secret", -time.Minute) // already expired on issue

	tok, _ := iss.Issue(1)
	if _, err := iss.Parse(tok); err == nil {
		t.Error("expected error for expired token, got nil")
	}
}
