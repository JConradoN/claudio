package pipeline

import (
	"strings"
	"testing"
)

func TestRedactSecrets_BearerToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
	}{
		{name: "Authorization Bearer", input: "Authorization: Bearer sk-abc123def456ghi789jkl012"},
		{name: "Authorization lowercase bearer", input: "authorization: bearer xyz789tokenvaluehere"},
		{name: "Authorization Basic", input: "Authorization: Basic dXNlcjpwYXNzd29yZA=="},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactSecrets(tc.input)
			if strings.Contains(got, "sk-abc123def456ghi789jkl012") {
				t.Errorf("Bearer token not redacted in: %s", got)
			}
			if strings.Contains(got, "dXNlcjpwYXNzd29yZA==") {
				t.Errorf("Basic auth not redacted in: %s", got)
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Errorf("expected [REDACTED] marker, got: %s", got)
			}
		})
	}
}

func TestRedactSecrets_AccessToken(t *testing.T) {
	t.Parallel()

	input := "access_token = eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dKcGZrKQgQ"
	got := redactSecrets(input)
	if strings.Contains(got, "eyJhbGciOiJIUzI1NiJ9") {
		t.Errorf("access_token value not redacted: %s", got)
	}
	if !strings.Contains(got, "[CREDENTIAL_REDACTED]") {
		t.Errorf("expected [CREDENTIAL_REDACTED], got: %s", got)
	}
}

func TestRedactSecrets_RefreshToken(t *testing.T) {
	t.Parallel()

	input := "refresh_token=r1c2k3e4r5t6y7u8i9o0p1a2s3d4f5g6h7j8k9l0"
	got := redactSecrets(input)
	if !strings.Contains(got, "[CREDENTIAL_REDACTED]") {
		t.Errorf("refresh_token not redacted: %s", got)
	}
}

func TestRedactSecrets_APIKeyCamelCase(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
	}{
		{name: "apiKey JSON style", input: `"apiKey": "sk-proj-abc123def456"`},
		{name: "clientSecret", input: "clientSecret: s3cr3t-cli3nt-v4lu3"},
		{name: "token generic", input: "token = ghp_abc123def456ghi789jkl012mno345pqr678stu901"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactSecrets(tc.input)
			if !strings.Contains(got, "[CREDENTIAL_REDACTED]") {
				t.Errorf("expected [CREDENTIAL_REDACTED], got: %s", got)
			}
		})
	}
}

func TestRedactSecrets_JSONSecret(t *testing.T) {
	t.Parallel()

	input := `{"apiKey":"sk-proj-abc123def456ghi789jkl012mno345","clientSecret":"super-secret-value-here"}`
	got := redactSecrets(input)
	if strings.Contains(got, "sk-proj-abc123def456ghi789jkl012mno345") {
		t.Errorf("API key in JSON not redacted: %s", got)
	}
	if strings.Contains(got, "super-secret-value-here") {
		t.Errorf("clientSecret in JSON not redacted: %s", got)
	}
}

func TestRedactSecrets_MultilinePrivateKey(t *testing.T) {
	t.Parallel()

	input := `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA0gI5j5KfV6XJzVZRyqFsnJFKxGcVJRL
f8KzLkXKsQVFxJwRwP0zI5Vf6XJzVZRyqFsnJFKxGcVJRL
-----END RSA PRIVATE KEY-----`
	got := redactSecrets(input)
	if strings.Contains(got, "MIIEpAIBAAKCAQEA0gI5j5KfV6XJzVZRyqFsnJFKxGcVJRL") {
		t.Errorf("private key body not redacted: %s", got)
	}
	if !strings.Contains(got, "[PRIVATE_KEY_BLOCK_REDACTED]") {
		t.Errorf("expected [PRIVATE_KEY_BLOCK_REDACTED], got: %s", got)
	}
}

func TestRedactSecrets_OpenSSHPrivateKey(t *testing.T) {
	t.Parallel()

	input := `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAABFwAAAAdzc2gtcn
NhAAAAAwEAAQAAAQEA0gI5j5KfV6XJzVZRyqFsnJFKxGcVJRLf8KzLkXKsQVFxJwRwP0
-----END OPENSSH PRIVATE KEY-----`
	got := redactSecrets(input)
	if strings.Contains(got, "b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAABFwAAAAdzc2gtcn") {
		t.Errorf("OpenSSH private key body not redacted: %s", got)
	}
	if !strings.Contains(got, "[PRIVATE_KEY_BLOCK_REDACTED]") {
		t.Errorf("expected [PRIVATE_KEY_BLOCK_REDACTED], got: %s", got)
	}
}

func TestRedactSecrets_MixedContent(t *testing.T) {
	t.Parallel()

	input := `Logging in with Authorization: Bearer sk-ant-abc123def456ghi789jkl012mno345
Response: {"apiKey":"live_secret_key_here","status":"ok"}
Private key: -----BEGIN EC PRIVATE KEY-----
MHQCAQEEIIm3VYQvHv+2xM7aBCzPBzXqB5VK
-----END EC PRIVATE KEY-----`
	got := redactSecrets(input)
	if strings.Contains(got, "sk-ant-abc123def456ghi789jkl012mno345") {
		t.Errorf("Bearer token not redacted")
	}
	if strings.Contains(got, "live_secret_key_here") {
		t.Errorf("JSON secret not redacted")
	}
	if strings.Contains(got, "MHQCAQEEIIm3VYQvHv+2xM7aBCzPBzXqB5VK") {
		t.Errorf("EC private key not redacted")
	}
}

func TestRedactSecrets_CheckpointContent(t *testing.T) {
	t.Parallel()

	// Simulate assistant output that could appear in a checkpoint.
	// completeRunLog calls redactSecrets on checkpoint + errMsg before storage.
	input := `Assistant response:
The API key for the project is sk-proj-abc123def4567890abcdef. 
I used Authorization: Bearer ghp_abc123def456ghi789jkl012mno345pqr678stu901 to access the API.
The config has "apiKey": "live_secret_key_here" and access_token=xyz789abc.
Private key at /home/user/.ssh/id_rsa contains:
-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAABFwAAAAdzc2gtcn
NhAAAAAwEAAQAAAQEA0gI5j5KfV6XJzVZRyqFsnJFKxGcVJRLf8KzLkXKsQVFxJwRwP0
-----END OPENSSH PRIVATE KEY-----`

	got := redactSecrets(input)

	if strings.Contains(got, "sk-proj-abc123def4567890abcdef") {
		t.Errorf("API key in checkpoint not redacted")
	}
	if strings.Contains(got, "ghp_abc123def456ghi789jkl012mno345pqr678stu901") {
		t.Errorf("GitHub token in checkpoint not redacted")
	}
	if strings.Contains(got, "live_secret_key_here") {
		t.Errorf("JSON apiKey in checkpoint not redacted")
	}
	if strings.Contains(got, "xyz789abc") {
		t.Errorf("access_token value in checkpoint not redacted")
	}
	if strings.Contains(got, "b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAABFwAAAAdzc2gtcn") {
		t.Errorf("private key body in checkpoint not redacted")
	}
}

func TestRedactSecrets_ExistingPatternsStillWork(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		check func(got string) bool
	}{
		{name: "OpenAI key", input: "sk-proj-abc123def4567890abcdef", check: func(got string) bool {
			return strings.Contains(got, "[API_KEY_REDACTED]")
		}},
		{name: "AWS key", input: "AKIAIOSFODNN7EXAMPLE", check: func(got string) bool {
			return strings.Contains(got, "[AWS_KEY_REDACTED]")
		}},
		{name: "JWT", input: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dKcGZrKQgQ", check: func(got string) bool {
			return strings.Contains(got, "[JWT_REDACTED]")
		}},
		{name: "GitHub PAT", input: "github_pat_abc123def456ghi789jkl012mno345pqr678stu901vwx234", check: func(got string) bool {
			return strings.Contains(got, "[GH_PAT_REDACTED]")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactSecrets(tc.input)
			if !tc.check(got) {
				t.Errorf("expected redaction in: %s", got)
			}
		})
	}
}

func TestRedactSecrets_QuotedSnakeCaseJSON(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
	}{
		{
			name:  `"api_key" in JSON`,
			input: `{"api_key": "sk-proj-abc123def456ghi789jkl012mno345"}`,
		},
		{
			name:  `"api-key" in JSON (kebab)`,
			input: `{"api-key": "sk-proj-abc123def456ghi789jkl012mno345"}`,
		},
		{
			name:  `"api.key" in JSON (dot)`,
			input: `{"api.key": "sk-proj-abc123def456ghi789jkl012mno345"}`,
		},
		{
			name:  `"client_secret" in JSON`,
			input: `{"client_secret": "s3cr3t-cli3nt-v4lu3-here"}`,
		},
		{
			name:  `"access_token" in JSON`,
			input: `{"access_token": "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dKcGZrKQgQ"}`,
		},
		{
			name:  `"refresh_token" in JSON`,
			input: `{"refresh_token": "r1c2k3e4r5t6y7u8i9o0p"}`,
		},
		{
			name:  `"client-secret" in JSON (kebab)`,
			input: `{"client-secret": "s3cr3t-cli3nt-v4lu3"}`,
		},
		{
			name:  `"client.secret" in JSON (dot)`,
			input: `{"client.secret": "s3cr3t-cli3nt-v4lu3"}`,
		},
		{
			name:  `"access-token" in JSON (kebab)`,
			input: `{"access-token": "xyz789abc"}`,
		},
		{
			name:  `"access.token" in JSON (dot)`,
			input: `{"access.token": "xyz789abc"}`,
		},
		{
			name:  `"refresh-token" in JSON (kebab)`,
			input: `{"refresh-token": "r1c2k3e4r5t6y7u8i9o0p"}`,
		},
		{
			name:  `"refresh.token" in JSON (dot)`,
			input: `{"refresh.token": "r1c2k3e4r5t6y7u8i9o0p"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactSecrets(tc.input)
			if strings.Contains(got, tc.input) {
				t.Errorf("JSON secret not redacted: %s", got)
			}
			if !strings.Contains(got, "[CREDENTIAL_REDACTED]") {
				t.Errorf("expected [CREDENTIAL_REDACTED] marker, got: %s", got)
			}
		})
	}
}

func TestRedactSecrets_ClientSecretUnquoted(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
	}{
		{name: "client_secret = value", input: "client_secret = s3cr3t-cli3nt-v4lu3"},
		{name: "client_secret: value", input: "client_secret: my-secret-value-here"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactSecrets(tc.input)
			if !strings.Contains(got, "[CREDENTIAL_REDACTED]") {
				t.Errorf("expected [CREDENTIAL_REDACTED] for %q, got: %s", tc.input, got)
			}
		})
	}
}

func TestRedactSecrets_FalsePositives(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
	}{
		{name: "word password in prose", input: "Please enter your password to continue."},
		{name: "token in prose", input: "the token is valid for 24 hours"},
		{name: "secret in prose no value", input: "the secret was not found"},
		{name: "api_key in prose", input: "the api_key was configured"},
		{name: "Authorization as word", input: "We need authorization from the admin"},
		{name: "private key path", input: "Located at /home/user/.ssh/id_rsa"},
		{name: "colon in prose", input: "Remember: the password manager is secure"},
		{name: "equals sign in prose", input: "I think the key is important"},
		{name: "token length mention", input: "the token must be 32 characters"},
		{name: "access in path", input: "Open access_token_manager.py to configure"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactSecrets(tc.input)
			if got != tc.input {
				t.Errorf("false positive: input=%q got=%q", tc.input, got)
			}
		})
	}
}
