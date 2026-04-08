package main

import (
	"testing"
)

func TestWizardEnvEnabled(t *testing.T) {
	t.Setenv("QBITTY_WIZARD", "")
	t.Setenv("WIZARD", "")
	if wizardEnvEnabled() {
		t.Fatal("expected false")
	}
	t.Setenv("QBITTY_WIZARD", "1")
	if !wizardEnvEnabled() {
		t.Fatal("expected true for QBITTY_WIZARD=1")
	}
	t.Setenv("QBITTY_WIZARD", "")
	t.Setenv("WIZARD", "yes")
	if !wizardEnvEnabled() {
		t.Fatal("expected true for WIZARD=yes")
	}
}
