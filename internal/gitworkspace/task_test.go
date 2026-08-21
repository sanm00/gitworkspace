package gitworkspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestReviewTaskIsInspectionFirst(t *testing.T) {
	project := Project{Name: "Demo"}
	workspace := Workspace{Name: "review-a", Type: WorkspaceReview, WorktreePath: "/tmp/w", Source: SourceSpec{Ref: "feature/a", BaseRef: "main"}}
	text := RenderTask(project, workspace)
	for _, needle := range []string{"Type: Review", "Target: feature/a", "Base: main", "Do not modify"} {
		if !strings.Contains(text, needle) {
			t.Fatalf("missing %q in %s", needle, text)
		}
	}
}

func TestSlug(t *testing.T) {
	if got := slug(" Feature/Hello World "); got != "feature-hello-world" {
		t.Fatalf("slug=%q", got)
	}
}

func TestSlugTruncatesAtRuneBoundary(t *testing.T) {
	for _, test := range []struct {
		name, input, expected string
	}{
		{name: "multibyte final rune", input: strings.Repeat("a", 47) + "界", expected: strings.Repeat("a", 47) + "界"},
		{name: "more than 48 runes", input: strings.Repeat("界", 60), expected: strings.Repeat("界", 48)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := slug(test.input)
			if got != test.expected {
				t.Fatalf("slug=%q want=%q", got, test.expected)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("slug is not valid UTF-8: %q", got)
			}
			if utf8.RuneCountInString(got) > 48 {
				t.Fatalf("slug has more than 48 runes: %d", utf8.RuneCountInString(got))
			}
		})
	}
}

func FuzzSlugAlwaysReturnsValidUTF8(f *testing.F) {
	for _, seed := range []string{"feature/avatar", strings.Repeat("界", 60), "a/b c", "🚀 release"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		got := slug(input)
		if !utf8.ValidString(got) {
			t.Fatalf("slug is not valid UTF-8: %q", got)
		}
		if utf8.RuneCountInString(got) > 48 {
			t.Fatalf("slug has more than 48 runes: %d", utf8.RuneCountInString(got))
		}
		if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
			t.Fatalf("slug has an outer dash: %q", got)
		}
	})
}

func TestWriteTaskDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.md")
	if err := WriteTask(path, "first"); err != nil {
		t.Fatal(err)
	}
	if err := WriteTask(path, "second"); err == nil {
		t.Fatal("expected exclusive-create error")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "first" {
		t.Fatalf("task overwritten: %q", data)
	}
}
