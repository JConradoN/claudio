package telegram

import "testing"

func TestExtractFrontmatterField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		content string
		field   string
		want    string
	}{
		{
			name: "simple value",
			content: `---
name: my-project
---
Some content`,
			field: "name",
			want: "my-project",
		},
		{
			name: "no frontmatter",
			content: `Just content.
No frontmatter here.`,
			field: "name",
			want: "",
		},
		{
			name: "empty frontmatter",
			content: `---
---`,
			field: "name",
			want: "",
		},
		{
			name: "field not present",
			content: `---
other: value
---
Content`,
			field: "name",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractFrontmatterField(tc.content, tc.field)
			if got != tc.want {
				t.Fatalf("extractFrontmatterField(%q, %q) = %q, want %q", tc.content, tc.field, got, tc.want)
			}
		})
	}
}
