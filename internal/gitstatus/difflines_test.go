package gitstatus

import (
	"reflect"
	"testing"
)

func TestParseDiffHunks(t *testing.T) {
	tests := []struct {
		name string
		diff string
		want map[int]LineChange
	}{
		{
			name: "pure addition",
			diff: `diff --git a/foo.go b/foo.go
index abc..def 100644
--- a/foo.go
+++ b/foo.go
@@ -10,0 +11,3 @@ func existing()
+line1
+line2
+line3
`,
			want: map[int]LineChange{
				11: LineAdded,
				12: LineAdded,
				13: LineAdded,
			},
		},
		{
			name: "modification replaces lines",
			diff: `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -5,2 +5,2 @@ package main
+newA
+newB
`,
			want: map[int]LineChange{
				5: LineModified,
				6: LineModified,
			},
		},
		{
			name: "pure deletion",
			diff: `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -5,3 +5,0 @@ package main
-old1
-old2
-old3
`,
			want: nil, // no new lines, nothing to highlight
		},
		{
			name: "multiple hunks",
			diff: `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,1 +1,1 @@
-old
+new
@@ -20,0 +20,2 @@ func bar()
+added1
+added2
`,
			want: map[int]LineChange{
				1:  LineModified,
				20: LineAdded,
				21: LineAdded,
			},
		},
		{
			name: "single line no count",
			diff: `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1 +1 @@
-old
+new
`,
			want: map[int]LineChange{
				1: LineModified,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDiffHunks([]byte(tt.diff))
			if tt.want == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("length mismatch: got %d, want %d\ngot: %v", len(got), len(tt.want), got)
				return
			}
			for line, wantType := range tt.want {
				if got[line] != wantType {
					t.Errorf("line %d: got %q, want %q", line, got[line], wantType)
				}
			}
		})
	}
}

func TestParseDeletionsFromDiff(t *testing.T) {
	tests := []struct {
		name  string
		diff  string
		hunks []DiffHunk
		want  []DiffDeletion
	}{
		{
			name: "pure deletion",
			diff: `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -5,3 +5,0 @@ package main
-old1
-old2
-old3
`,
			hunks: []DiffHunk{
				{StartLine: 5, EndLine: 4, Diff: "-old1\n-old2\n-old3\n"},
			},
			want: []DiffDeletion{
				{AfterLine: 5, Count: 3, HunkIndex: 0},
			},
		},
		{
			name: "no deletions in modification",
			diff: `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -5,2 +5,2 @@ package main
-old
+new
`,
			hunks: []DiffHunk{
				{StartLine: 5, EndLine: 6},
			},
			want: nil,
		},
		{
			name: "deletion at top of file",
			diff: `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,2 +1,0 @@
-line1
-line2
`,
			hunks: []DiffHunk{
				{StartLine: 1, EndLine: 0, Diff: "-line1\n-line2\n"},
			},
			want: []DiffDeletion{
				{AfterLine: 1, Count: 2, HunkIndex: 0},
			},
		},
		{
			name:  "empty diff",
			diff:  "",
			hunks: []DiffHunk{{StartLine: 1, EndLine: 5}},
			want:  nil,
		},
		{
			name: "multiple deletions",
			diff: `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -3,1 +3,0 @@
-removed1
@@ -10,2 +9,0 @@
-removed2
-removed3
`,
			hunks: []DiffHunk{
				{StartLine: 3, EndLine: 2, Diff: "-removed1\n"},
				{StartLine: 9, EndLine: 8, Diff: "-removed2\n-removed3\n"},
			},
			want: []DiffDeletion{
				{AfterLine: 3, Count: 1, HunkIndex: 0},
				{AfterLine: 9, Count: 2, HunkIndex: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDeletionsFromDiff([]byte(tt.diff), tt.hunks)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
