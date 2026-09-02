package main

import (
	"reflect"
	"testing"
)

func TestBinarySignArguments(t *testing.T) {
	tests := []struct {
		name     string
		identity string
		want     []string
	}{
		{
			name:     "ad hoc",
			identity: "-",
			want:     []string{"--force", "--sign", "-", "--timestamp=none", "/tmp/outrider"},
		},
		{
			name:     "developer id",
			identity: "Developer ID Application: Corvine Systems",
			want: []string{
				"--force", "--sign", "Developer ID Application: Corvine Systems",
				"--options", "runtime", "--timestamp", "/tmp/outrider",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := binarySignArguments(test.identity, "/tmp/outrider")
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("binary signing arguments = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestDiskImageSignArguments(t *testing.T) {
	tests := []struct {
		name     string
		identity string
		want     []string
	}{
		{
			name:     "ad hoc",
			identity: "-",
			want:     []string{"--force", "--sign", "-", "--timestamp=none", "/tmp/Outrider.dmg"},
		},
		{
			name:     "developer id",
			identity: "Developer ID Application: Corvine Systems",
			want: []string{
				"--force", "--sign", "Developer ID Application: Corvine Systems",
				"--timestamp", "/tmp/Outrider.dmg",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := diskImageSignArguments(test.identity, "/tmp/Outrider.dmg")
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("disk image signing arguments = %#v, want %#v", got, test.want)
			}
		})
	}
}
