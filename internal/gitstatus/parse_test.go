package gitstatus

import (
	"testing"

	"github.com/yacchi/statusloom/internal/schema"
)

func TestParseStatusPorcelainV2(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  schema.RepositorySnapshot
	}{
		{
			name: "normal branch with ahead/behind, clean",
			input: "# branch.oid abcdef1234567890\n" +
				"# branch.head main\n" +
				"# branch.upstream origin/main\n" +
				"# branch.ab +2 -1\n",
			want: schema.RepositorySnapshot{
				Branch: "main",
				Ahead:  2,
				Behind: 1,
			},
		},
		{
			name: "detached HEAD with oid",
			input: "# branch.oid abcdef1234567890\n" +
				"# branch.head (detached)\n",
			want: schema.RepositorySnapshot{
				Branch: "abcdef1",
			},
		},
		{
			name:  "detached HEAD without oid falls back to label",
			input: "# branch.head (detached)\n",
			want: schema.RepositorySnapshot{
				Branch: "(detached)",
			},
		},
		{
			name: "staged only",
			input: "# branch.head main\n" +
				"1 M. N... 100644 100644 100644 aaaa bbbb file.txt\n",
			want: schema.RepositorySnapshot{
				Branch: "main",
				Staged: 1,
				Dirty:  true,
			},
		},
		{
			name: "unstaged only",
			input: "# branch.head main\n" +
				"1 .M N... 100644 100644 100644 aaaa bbbb file.txt\n",
			want: schema.RepositorySnapshot{
				Branch:   "main",
				Unstaged: 1,
				Dirty:    true,
			},
		},
		{
			name: "staged and unstaged on same entry",
			input: "# branch.head main\n" +
				"1 MM N... 100644 100644 100644 aaaa bbbb file.txt\n",
			want: schema.RepositorySnapshot{
				Branch:   "main",
				Staged:   1,
				Unstaged: 1,
				Dirty:    true,
			},
		},
		{
			name: "untracked",
			input: "# branch.head main\n" +
				"? newfile.txt\n",
			want: schema.RepositorySnapshot{
				Branch:    "main",
				Untracked: 1,
				Dirty:     true,
			},
		},
		{
			name: "unmerged entry",
			input: "# branch.head main\n" +
				"u UU N... 100644 100644 100644 100644 aaaa bbbb cccc file.txt\n",
			want: schema.RepositorySnapshot{
				Branch:   "main",
				Unstaged: 1,
				Dirty:    true,
			},
		},
		{
			name: "renamed entry",
			input: "# branch.head main\n" +
				"2 R. N... 100644 100644 100644 aaaa bbbb R100 newname.txt\toldname.txt\n",
			want: schema.RepositorySnapshot{
				Branch: "main",
				Staged: 1,
				Dirty:  true,
			},
		},
		{
			name:  "missing branch.ab (no upstream)",
			input: "# branch.head main\n",
			want: schema.RepositorySnapshot{
				Branch: "main",
				Ahead:  0,
				Behind: 0,
			},
		},
		{
			name:  "empty output",
			input: "",
			want:  schema.RepositorySnapshot{},
		},
		{
			name: "truncated last line is ignored",
			input: "# branch.head main\n" +
				"1 M. N... 100644 100644 100644 aaaa bbbb file.txt\n" +
				"? partial-file-name-that-got-cu",
			want: schema.RepositorySnapshot{
				Branch: "main",
				Staged: 1,
				Dirty:  true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseStatusPorcelainV2([]byte(tt.input))
			if got != tt.want {
				t.Errorf("parseStatusPorcelainV2(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseAheadBehind(t *testing.T) {
	tests := []struct {
		input      string
		wantAhead  int
		wantBehind int
	}{
		{"+2 -1", 2, 1},
		{"+0 -0", 0, 0},
		{"-3 +4", 4, 3},
		{"", 0, 0},
		{"garbage", 0, 0},
	}
	for _, tt := range tests {
		ahead, behind := parseAheadBehind(tt.input)
		if ahead != tt.wantAhead || behind != tt.wantBehind {
			t.Errorf("parseAheadBehind(%q) = (%d, %d), want (%d, %d)", tt.input, ahead, behind, tt.wantAhead, tt.wantBehind)
		}
	}
}

func TestSplitCompleteLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"single terminated line", "a\n", []string{"a"}},
		{"multiple terminated lines", "a\nb\nc\n", []string{"a", "b", "c"}},
		{"truncated last line", "a\nb\npartial", []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitCompleteLines([]byte(tt.input))
			if len(got) != len(tt.want) {
				t.Fatalf("splitCompleteLines(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("splitCompleteLines(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}
