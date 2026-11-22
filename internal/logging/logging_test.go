package logging

import "testing"

func TestVerbosity(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int
	}{
		{name: "unset uses the default", value: "", want: DefaultVerbosity},
		{name: "whitespace only uses the default", value: "  ", want: DefaultVerbosity},
		{name: "info", value: "1", want: 1},
		{name: "debug", value: "2", want: 2},
		{name: "silent", value: "-4", want: -4},
		{name: "surrounding whitespace is trimmed", value: " 0 ", want: 0},
		{name: "above debug is clamped", value: "9", want: MaxVerbosity},
		{name: "below silent is clamped", value: "-9", want: MinVerbosity},
		{name: "non-numeric uses the default", value: "info", want: DefaultVerbosity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string {
				if key != EnvVerbosity {
					t.Fatalf("unexpected env lookup %q", key)
				}
				return tt.value
			}
			if got := Verbosity(getenv); got != tt.want {
				t.Errorf("Verbosity(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}
