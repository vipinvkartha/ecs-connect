package cloud

import "testing"

func Test_arnName(t *testing.T) {
	tests := []struct {
		arn, want string
	}{
		{"", ""},
		{"short", "short"},
		{"arn:aws:ecs:eu-west-1:123:cluster/my-cluster", "my-cluster"},
		{"arn:aws:ecs:eu-west-1:123:service/my-svc/my-service", "my-service"},
		{"arn:aws:ecs:eu-west-1:123:task/my-cluster/abc123def", "abc123def"},
		{"/trailing/slash/", ""},
		{"no-slash-resource-name", "no-slash-resource-name"},
	}
	for _, tt := range tests {
		if got := arnName(tt.arn); got != tt.want {
			t.Errorf("arnName(%q) = %q, want %q", tt.arn, got, tt.want)
		}
	}
}
