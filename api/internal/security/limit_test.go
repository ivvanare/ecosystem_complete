package security

import (
	"errors"
	"testing"
	"time"
)

var quotaNow = time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)

func TestQuotaAllowsTheFirstRequest(t *testing.T) {
	if err := (Quota{}).Check(quotaNow, nil, 0); err != nil {
		t.Fatalf("first request rejected: %v", err)
	}
}

func TestQuotaEnforcesTheCooldown(t *testing.T) {
	last := quotaNow.Add(-10 * time.Second)

	err := Quota{Cooldown: time.Minute}.Check(quotaNow, &last, 1)

	var tooMany *TooManyRequestsError
	if !errors.As(err, &tooMany) {
		t.Fatalf("error = %v, want *TooManyRequestsError", err)
	}
	if tooMany.RetryAfter != 50*time.Second {
		t.Errorf("RetryAfter = %v, want the 50s left in the cooldown", tooMany.RetryAfter)
	}
}

func TestQuotaAllowsAfterTheCooldown(t *testing.T) {
	last := quotaNow.Add(-2 * time.Minute)

	if err := (Quota{Cooldown: time.Minute}).Check(quotaNow, &last, 1); err != nil {
		t.Fatalf("request after the cooldown rejected: %v", err)
	}
}

func TestQuotaEnforcesTheWindowLimit(t *testing.T) {
	last := quotaNow.Add(-10 * time.Minute)

	err := Quota{Cooldown: time.Minute, Window: 24 * time.Hour, Limit: 5}.Check(quotaNow, &last, 5)

	var tooMany *TooManyRequestsError
	if !errors.As(err, &tooMany) {
		t.Fatalf("error = %v, want *TooManyRequestsError", err)
	}
	if tooMany.RetryAfter != 24*time.Hour {
		t.Errorf("RetryAfter = %v, want the window", tooMany.RetryAfter)
	}
}

func TestQuotaUsesDefaultsWhenUnset(t *testing.T) {
	last := quotaNow.Add(-30 * time.Second)

	if err := (Quota{}).Check(quotaNow, &last, 1); err == nil {
		t.Error("an empty quota allowed a request inside the default cooldown")
	}
	if err := (Quota{}).Check(quotaNow, nil, DefaultLimit); err == nil {
		t.Error("an empty quota ignored the default limit")
	}
}
