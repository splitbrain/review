package gitstatus

import (
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
