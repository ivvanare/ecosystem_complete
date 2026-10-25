package probe

import "testing"

// The readiness contract depends on these flags: a required probe failing turns the
// report into "unavailable"; an optional one only degrades it.
func TestRequirednessMatchesTheReadinessContract(t *testing.T) {
	cases := []struct {
		name     string
		required bool
	}{
		{name: Postgres{}.Name(), required: Postgres{}.Required()},
		{name: Redis{}.Name(), required: Redis{}.Required()},
		{name: RabbitMQ{}.Name(), required: RabbitMQ{}.Required()},
	}

	want := map[string]bool{
		"postgres": true,
		"redis":    true,
		"rabbitmq": false,
	}

	for _, tc := range cases {
		expected, ok := want[tc.name]
		if !ok {
			t.Fatalf("unexpected probe name %q", tc.name)
		}
		if tc.required != expected {
			t.Errorf("%s.Required() = %v, want %v", tc.name, tc.required, expected)
		}
	}
}
