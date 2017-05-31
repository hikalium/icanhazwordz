package words

import (
	"bytes"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{{"fish", "FISH"},
		{"FiSH", "FISH"},
		{"", ""},
		{"quarter", "QARTER"},
		{"Qatar", "QATAR"},
	} {
		got := Normalize(tc.in)
		if got != tc.want {
			t.Errorf("Normalize(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func accum(ch chan string) []string {
	var res []string
	for s := range ch {
		res = append(res, s)
	}
	return res
}

func TestLoadValid(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want []string
	}{
		{[]string{"foo", "bar", "123", ""}, []string{"foo", "bar"}},
		{[]string{"quart", "quartz", "quarter"}, []string{"quart"}},
		{[]string{"'", "étude", "al's"}, []string{}},
	} {
		ch := make(chan string)
		go func() {
			LoadValid(bytes.NewBufferString(strings.Join(tc.in, "\n")), 4, ch)
			close(ch)
		}()
		got := accum(ch)
		gotJoin := strings.Join(got, ";")
		wantJoin := strings.Join(tc.want, ";")
		if gotJoin != wantJoin {
			t.Errorf("LoadValid(%v) = %v want %v", tc.in, got, tc.want)
		}
	}
}

func countAll(ss []string) []countMap {
	var counts []countMap
	for _, s := range ss {
		counts = append(counts, Count(s))
	}
	return counts
}

func TestContains(t *testing.T) {
	for _, tc := range []struct {
		haystack string
		in       []string
		out      []string
	}{
		{"astronomer", []string{"moon", "starer"}, []string{"noodle"}},
		{"quartera", []string{"qatar", "quart"}, []string{"quartz"}},
	} {
		count := Count(tc.haystack)
		for _, in := range tc.in {
			if !count.Contains(Count(in)) {
				t.Errorf("Count(%q).Contains(%q) = false want true", tc.haystack, in)
			}
		}
		for _, out := range tc.out {
			if count.Contains(Count(out)) {
				t.Errorf("Count(%q).Contains(%q) = true want false", tc.haystack, out)
			}
		}
		// Test Max too while we're at it.
		all := append([]string{tc.haystack}, tc.out...)
		all = append(all, tc.in...)
		cover := Max(countAll(all)...)
		for _, s := range all {
			if !cover.Contains(Count(s)) {
				t.Errorf("Max(%v).Contains(%q) = false want true", all, s)
			}
		}
	}
}

func TestCountString(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"moon", "MNOO"},
		{"astronomer", "AEMNOORRST"},
	} {
		got := Count(tc.in).String()
		if got != tc.want {
			t.Errorf("Count(%q) = %v want %v", tc.in, got, tc.want)
		}
	}
}
