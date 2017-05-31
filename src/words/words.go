package words

import (
	"bufio"
	"io"
	"math/rand"
	"os"
	"regexp"
	"sort"
	"strings"
)

type countMap map[string]int

var (
	qFixer   = strings.NewReplacer("QU", "Q")
	qUnfixer = strings.NewReplacer("Q", "Qu")
	wordRE   = regexp.MustCompile(`^(?i:(?:[a-pr-z])|qu){3,}$`)
	rejectRE = regexp.MustCompile(`^[A-Z]`)
)

const (
	kMinLen = 3
)

func Normalize(word string) string {
	return qFixer.Replace(strings.ToUpper(word))
}

func Denormalize(word string) string {
	return qUnfixer.Replace(Normalize(word))
}

func Count(word string) countMap {
	return CountLetters(strings.Split(Normalize(word), ""))
}

func CountLetters(letters []string) countMap {
	c := make(countMap)
	for _, l := range letters {
		c[l]++
	}
	return c
}

func LoadValidFile(fileName string, maxLen int, ch chan string) error {
	file, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer file.Close()
	LoadValid(file, maxLen, ch)
	return nil
}

func LoadValid(reader io.Reader, maxLen int, ch chan string) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		word := scanner.Text()
		if !wordRE.MatchString(word) {
			continue
		}
		norm := Normalize(word)
		if len(norm) > maxLen || len(norm) < kMinLen {
			continue
		}
		ch <- word
	}
}

// Finds the maximum of each letter count for a set of countMaps.  In
// other words, the result of Max(a,b,c) would necessarily Contain any
// of a b and c for any a b c.
func Max(counts ...countMap) countMap {
	res := make(countMap)
	for _, cm := range counts {
		for l, c := range cm {
			if res[l] > c {
				continue
			}
			res[l] = c
		}
	}
	return res
}

func (haystack countMap) Contains(needle countMap) bool {
	for l, c := range needle {
		if haystack[l] < c {
			return false
		}
	}
	return true
}

func (cm countMap) String() string {
	var letters []string
	for l, c := range cm {
		for i := 0; i < c; i++ {
			letters = append(letters, l)
		}
	}
	sort.Sort(sort.StringSlice(letters))
	return strings.Join(letters, "")
}

type cdfPoint struct {
	letter string
	mass   int64
}

type LetterGen struct {
	freqs map[string]int64
	cdf   []cdfPoint
	total int64
}

func NewLetterGenCorpus(dict map[string]string) LetterGen {
	freqs := make(map[string]int64)
	for norm := range dict {
		for l, c := range Count(norm) {
			t := int64(c)
			freqs[l] += t
		}
	}
	return NewLetterGenFreqs(freqs)
}

func NewLetterGenFreqs(freqs map[string]int64) LetterGen {
	var res LetterGen
	res.freqs = freqs
	// need to put these in a deterministic order
	var letters []string
	for l, c := range res.freqs {
		letters = append(letters, l)
		res.total += c
	}
	sort.Sort(sort.StringSlice(letters))
	mass := int64(0)
	for _, l := range letters {
		mass += res.freqs[l]
		res.cdf = append(res.cdf, cdfPoint{l, mass})
	}
	return res
}

func (g LetterGen) Next(rng *rand.Rand) string {
	x := rng.Int63n(g.total)
	for _, c := range g.cdf {
		if x < c.mass {
			return c.letter
		}
	}
	panic("impossible")
}
