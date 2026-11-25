package unidiff

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestAddedListsEachFilesAddedLines(t *testing.T) {
	got, err := Added(strings.NewReader(`diff --git a/sidecar/a.go b/sidecar/a.go
index 1111111..2222222 100644
--- a/sidecar/a.go
+++ b/sidecar/a.go
@@ -3,0 +4,2 @@ func f() {
+	x := 1
+	_ = x
@@ -10 +12 @@ func g() {
-	return 1
+	return 2
@@ -20,3 +23,0 @@ func h() {
-	a
-	b
-	c
diff --git a/src/gone.ts b/src/gone.ts
deleted file mode 100644
--- a/src/gone.ts
+++ /dev/null
@@ -1,2 +0,0 @@
-a
-b
diff --git a/src/new.ts b/src/new.ts
new file mode 100644
--- /dev/null
+++ b/src/new.ts
@@ -0,0 +1,2 @@
+export const a = 1;
+export const b = 2;
`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]int{
		"sidecar/a.go": {4, 5, 12},
		"src/new.ts":   {1, 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Added = %v, want %v", got, want)
	}
}

func TestAddedReadsAHunkBodyByItsCounts(t *testing.T) {
	// The added "++ x" and "-- y" lines are content, and context lines advance
	// the new side.
	got, err := Added(strings.NewReader(`--- a/a.md
+++ b/a.md
@@ -1,3 +1,4 @@
 one
-two
+++ x
+--- y
 three
\ No newline at end of file
`))
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string][]int{"a.md": {2, 3}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Added = %v, want %v", got, want)
	}
}

func TestAddedRefusesABadHunkHeader(t *testing.T) {
	for _, header := range []string{"@@ -1 @@", "@@ 1 +1 @@", "@@ -x +1 @@", "@@ -1 +x @@", "@@ -1 +1,y @@"} {
		if _, err := Added(strings.NewReader("+++ b/a.go\n" + header + "\n")); err == nil {
			t.Errorf("%q: Added succeeded", header)
		}
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("broken") }

func TestAddedReturnsAReadError(t *testing.T) {
	if _, err := Added(failingReader{}); err == nil || err.Error() != "broken" {
		t.Errorf("Added = %v, want the read error", err)
	}
}

func TestAddedKeysAPathGitMarksByItsPathInTheTree(t *testing.T) {
	// Git ends a path holding a space with a tab, and C-quotes one holding a
	// tab, quote, backslash or non-ASCII byte.
	got, err := Added(strings.NewReader("--- /dev/null\n" +
		"+++ b/my file.go\t\n" +
		"@@ -0,0 +1 @@\n" +
		"+a\n" +
		"--- /dev/null\n" +
		`+++ "b/tab\tq\"uote\\\303\251.go"` + "\n" +
		"@@ -0,0 +1 @@\n" +
		"+a\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]int{"my file.go": {1}, "tab\tq\"uote\\é.go": {1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Added = %#v, want %#v", got, want)
	}
}

func TestAddedRefusesABadlyQuotedPath(t *testing.T) {
	if _, err := Added(strings.NewReader(`+++ "b/a.go` + "\n")); err == nil {
		t.Error("Added succeeded")
	}
}
