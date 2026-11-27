package software

import "testing"

func TestNameString(t *testing.T) {
	tests := []struct {
		in   Name
		want string
	}{
		{Geth, "Geth"},
		{Nethermind, "Nethermind"},
		{Lighthouse, "Lighthouse"},
		{Unknown, "unknown"},
		{Name("other"), "other"},
	}
	for _, tt := range tests {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("%q.String() = %q, want %q", string(tt.in), got, tt.want)
		}
	}
}
