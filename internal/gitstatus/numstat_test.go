package gitstatus

import "testing"

func TestParseNumstat(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantAdded   int
		wantDeleted int
	}{
		{
			name:        "single text file",
			input:       "3\t1\tfile.txt\n",
			wantAdded:   3,
			wantDeleted: 1,
		},
		{
			name: "multiple text files summed",
			input: "3\t1\tfile.txt\n" +
				"10\t0\tother.go\n",
			wantAdded:   13,
			wantDeleted: 1,
		},
		{
			name:        "binary file skipped",
			input:       "-\t-\timage.png\n",
			wantAdded:   0,
			wantDeleted: 0,
		},
		{
			name: "mixed binary and text",
			input: "-\t-\timage.png\n" +
				"5\t2\tfile.txt\n",
			wantAdded:   5,
			wantDeleted: 2,
		},
		{
			name:        "renamed file with arrow notation",
			input:       "1\t1\told.txt => new.txt\n",
			wantAdded:   1,
			wantDeleted: 1,
		},
		{
			name:        "empty output",
			input:       "",
			wantAdded:   0,
			wantDeleted: 0,
		},
		{
			name: "truncated last line ignored",
			input: "4\t2\tfile.txt\n" +
				"7\t3\tother",
			wantAdded:   4,
			wantDeleted: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			added, deleted := parseNumstat([]byte(tt.input))
			if added != tt.wantAdded || deleted != tt.wantDeleted {
				t.Errorf("parseNumstat(%q) = (%d, %d), want (%d, %d)", tt.input, added, deleted, tt.wantAdded, tt.wantDeleted)
			}
		})
	}
}
