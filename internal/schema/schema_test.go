package schema

import "testing"

func TestAnthropicBase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://fxqidian.de5.net/v1", "https://fxqidian.de5.net"},
		{"https://gorouter.app/v1", "https://gorouter.app"},
		{"https://agentrouter.org", "https://agentrouter.org"},
		{"https://gorouter.app/v1/", "https://gorouter.app"},
		{"https://api.example.com/v1/v1", "https://api.example.com/v1"},
		{"https://api.example.com/v1beta1", "https://api.example.com/v1beta1"},
	}
	for _, c := range cases {
		if got := AnthropicBase(c.in); got != c.want {
			t.Errorf("AnthropicBase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
