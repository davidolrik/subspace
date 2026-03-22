package pages

import (
	"testing"
)

func TestIsInternalHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"dashboard.subspace", true},
		{"statistics.subspace", true},
		{"anything.subspace", true},
		{"subspace.dk", false},
		{"subspace", false},
		{"example.com", false},
		{"dashboard.subspace.com", false},
		{"", false},
	}

	for _, tt := range tests {
		got := IsInternalHost(tt.host)
		if got != tt.want {
			t.Errorf("IsInternalHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}
