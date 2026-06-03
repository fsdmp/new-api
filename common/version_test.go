package common

import (
	"testing"
)

func TestParseSemVer(t *testing.T) {
	tests := []struct {
		input   string
		want    SemVer
		wantErr bool
	}{
		{"1.2.3", SemVer{1, 2, 3}, false},
		{"v1.2.3", SemVer{1, 2, 3}, false},
		{"V1.2.3", SemVer{1, 2, 3}, false},
		{"1.2", SemVer{1, 2, 0}, false},
		{"1", SemVer{1, 0, 0}, false},
		{"1.2.3-beta.1", SemVer{1, 2, 3}, false},
		{"1.2.3+build", SemVer{1, 2, 3}, false},
		{"v1.2.3-beta.1+build", SemVer{1, 2, 3}, false},
		{"  1.2.3  ", SemVer{1, 2, 3}, false},
		{"0.0.0", SemVer{0, 0, 0}, false},
		{"10.20.30", SemVer{10, 20, 30}, false},
		{"", SemVer{}, true},
		{"abc", SemVer{}, true},
		{"v", SemVer{}, true},
		{"1.a.3", SemVer{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSemVer(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseSemVer(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseSemVer(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got != tt.want {
				t.Errorf("ParseSemVer(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCompareSemVer(t *testing.T) {
	tests := []struct {
		a, b SemVer
		want int
	}{
		{SemVer{1, 0, 0}, SemVer{1, 0, 0}, 0},
		{SemVer{1, 2, 0}, SemVer{1, 2, 0}, 0},
		{SemVer{2, 0, 0}, SemVer{1, 0, 0}, 1},
		{SemVer{1, 0, 0}, SemVer{2, 0, 0}, -1},
		{SemVer{1, 2, 0}, SemVer{1, 1, 0}, 1},
		{SemVer{1, 1, 0}, SemVer{1, 2, 0}, -1},
		{SemVer{1, 0, 2}, SemVer{1, 0, 1}, 1},
		{SemVer{1, 0, 1}, SemVer{1, 0, 2}, -1},
	}

	for _, tt := range tests {
		got := CompareSemVer(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("CompareSemVer(%v, %v) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseAndCompareSemVer(t *testing.T) {
	tests := []struct {
		version, minimum string
		want             bool
		wantErr          bool
	}{
		{"1.3.0", "1.2.0", true, false},
		{"1.2.0", "1.2.0", true, false},
		{"0.9.0", "1.2.0", false, false},
		{"2.0.0", "1.9.9", true, false},
		{"1.2.3", "1.2.4", false, false},
		{"1.2.4", "1.2.3", true, false},
		{"abc", "1.2.0", false, true},
		{"1.2.0", "abc", false, true},
	}

	for _, tt := range tests {
		got, err := ParseAndCompareSemVer(tt.version, tt.minimum)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseAndCompareSemVer(%q, %q) expected error, got nil", tt.version, tt.minimum)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseAndCompareSemVer(%q, %q) unexpected error: %v", tt.version, tt.minimum, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseAndCompareSemVer(%q, %q) = %v, want %v", tt.version, tt.minimum, got, tt.want)
		}
	}
}
