package common

import "testing"

func TestInitTracelessUsers(t *testing.T) {
	InitTracelessUsers(" alice , bob ,,  ")
	if !IsTracelessUsername("alice") {
		t.Error("expected alice to be traceless")
	}
	if !IsTracelessUsername("bob") {
		t.Error("expected bob to be traceless")
	}
	if IsTracelessUsername("carol") {
		t.Error("carol should not be traceless")
	}
	if IsTracelessUsername("alice ") {
		t.Error("entries should be trimmed, no fuzzy matching")
	}
	if IsTracelessUsername("") {
		t.Error("empty username should never match")
	}
}

func TestInitTracelessUsersEmpty(t *testing.T) {
	InitTracelessUsers("")
	if IsTracelessUsername("alice") {
		t.Error("empty list should match nothing")
	}
}
