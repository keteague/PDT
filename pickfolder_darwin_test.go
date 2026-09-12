package main

import "testing"

func TestAppleScriptString(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Select Save File Base Path", `"Select Save File Base Path"`},
		{"embedded quote", `say "hi"`, `"say \"hi\""`},
		{"backslash", `C:\Drivers`, `"C:\\Drivers"`},
		{"quote and backslash", `\"`, `"\\\""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := appleScriptString(tt.in); got != tt.want {
				t.Errorf("appleScriptString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
