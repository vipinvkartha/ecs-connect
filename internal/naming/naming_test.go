package naming

import "testing"

func TestClusterMatchesEnv(t *testing.T) {
	tests := []struct {
		cluster, env string
		want         bool
	}{
		{"home-staging", "staging", true},
		{"home-staging", "production", false},
		{"accounts-production", "production", true},
		{"my-app-staging", "staging", true},
		{"foo", "staging", false},
		{"staging", "staging", false},
	}
	for _, tt := range tests {
		if got := ClusterMatchesEnv(tt.cluster, tt.env); got != tt.want {
			t.Errorf("ClusterMatchesEnv(%q, %q) = %v, want %v",
				tt.cluster, tt.env, got, tt.want)
		}
	}
}

func TestAppGroup(t *testing.T) {
	tests := []struct {
		cluster, env, want string
	}{
		{"home-staging", "staging", "home"},
		{"accounts-production", "production", "accounts"},
		{"my-app-staging", "staging", "my-app"},
	}
	for _, tt := range tests {
		if got := AppGroup(tt.cluster, tt.env); got != tt.want {
			t.Errorf("AppGroup(%q, %q) = %q, want %q",
				tt.cluster, tt.env, got, tt.want)
		}
	}
}

func TestServiceMatchesConvention(t *testing.T) {
	tests := []struct {
		service, appGroup, env string
		want                   bool
	}{
		{"home-staging", "home", "staging", true},
		{"home-worker-staging", "home", "staging", true},
		{"home-api-staging", "home", "staging", true},
		{"home-background-worker-staging", "home", "staging", true},
		{"other-staging", "home", "staging", false},
		{"home-staging", "home", "production", false},
		{"home-production", "home", "production", true},
	}
	for _, tt := range tests {
		if got := ServiceMatchesConvention(tt.service, tt.appGroup, tt.env); got != tt.want {
			t.Errorf("ServiceMatchesConvention(%q, %q, %q) = %v, want %v",
				tt.service, tt.appGroup, tt.env, got, tt.want)
		}
	}
}

func TestServiceToSlug(t *testing.T) {
	tests := []struct {
		service, appGroup, env, want string
	}{
		{"home-staging", "home", "staging", "web"},
		{"home-worker-staging", "home", "staging", "worker"},
		{"home-api-staging", "home", "staging", "api"},
		{"home-sidekiq-production", "home", "production", "sidekiq"},
		{"home-background-worker-staging", "home", "staging", "background-worker"},
	}
	for _, tt := range tests {
		if got := ServiceToSlug(tt.service, tt.appGroup, tt.env); got != tt.want {
			t.Errorf("ServiceToSlug(%q, %q, %q) = %q, want %q",
				tt.service, tt.appGroup, tt.env, got, tt.want)
		}
	}
}

func TestSlugToServiceName(t *testing.T) {
	tests := []struct {
		slug, appGroup, env, want string
	}{
		{"web", "home", "staging", "home-staging"},
		{"worker", "home", "staging", "home-worker-staging"},
		{"api", "home", "production", "home-api-production"},
		{"background-worker", "home", "staging", "home-background-worker-staging"},
	}
	for _, tt := range tests {
		if got := SlugToServiceName(tt.slug, tt.appGroup, tt.env); got != tt.want {
			t.Errorf("SlugToServiceName(%q, %q, %q) = %q, want %q",
				tt.slug, tt.appGroup, tt.env, got, tt.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		service, appGroup, env string
	}{
		{"home-staging", "home", "staging"},
		{"home-worker-staging", "home", "staging"},
		{"home-api-production", "home", "production"},
	}
	for _, tt := range cases {
		slug := ServiceToSlug(tt.service, tt.appGroup, tt.env)
		got := SlugToServiceName(slug, tt.appGroup, tt.env)
		if got != tt.service {
			t.Errorf("round-trip failed: %q → slug %q → %q (want %q)",
				tt.service, slug, got, tt.service)
		}
	}
}
