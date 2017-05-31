package lex

import (
	"bytes"
	crand "crypto/rand"
	"encoding/binary"
	"fmt"
	"html/template"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"words"

	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/user"
)

const (
	kHTML   = "text/html; charset=utf-8"
	kPlain  = "text/plain; charset=utf-8"
	kLen    = 4
	kSize   = kLen * kLen
	kHSKind = "HighScore"
)

var (
	templates *template.Template

	kGameLen = 10

	dict         map[string]string
	pointVals    map[string]int64
	pointLetters map[string][]string
	letterGen    words.LetterGen

	hostRE   = regexp.MustCompile(`^(?:.+\.)?(?:github.com|bitbucket.com)$`)
	schemeRE = regexp.MustCompile(`^https?$`)
)

type Page struct {
	Template   string
	Title      string
	Errors     []string
	Game       *Game
	HighScores []HighScore

	LoginURL  string
	LogoutURL string
	User      *user.User
	Admin     bool

	PointLetters map[string][]string
}

func (p *Page) maybeNotify(err error) {
	if err != nil {
		p.Errors = append(p.Errors, err.Error())
	}
}

func mustLoadDict(filename string) map[string]string {
	file, err := os.Open(filename)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	res := make(map[string]string)
	ch := make(chan string)
	go func() {
		words.LoadValid(file, kSize, ch)
		close(ch)
	}()
	for word := range ch {
		norm := words.Normalize(word)
		res[norm] = word
	}
	return res
}

func init() {
	if appengine.IsDevAppServer() {
		kGameLen = 4
	}
	var err error
	templates, err = template.New("").Funcs(template.FuncMap{
		"Zero": func(t time.Time) bool {
			return t.IsZero()
		},
		"Unix": func(t time.Time) int64 {
			return t.Unix()
		},
		"Now": func() int64 {
			return time.Now().Unix()
		},
		"FTime": func(t time.Time) string {
			return t.In(time.Local).Format("2006-01-02 15:04 MST")
		},
		"HumanDur": func(d time.Duration) time.Duration {
			return time.Second * time.Duration(d.Seconds())
		},
		"Board": func(g Game) [kLen][kLen]string {
			return g.Board()
		},
		"Move": func(move string) template.HTML {
			switch move {
			case "":
				return template.HTML("<span class=pass>&lt;PASS&gt;</span>")
			default:
				return template.HTML(fmt.Sprintf("<span class=normal>%v</span>", template.HTMLEscapeString(move)))
			}
		},
		"ScoreGame": func(g Game) int64 {
			return g.Score()
		},
		"Score": func(move string) int64 {
			return ScoreWord(move)
		},
		"Points": func(letter string) int64 { return pointVals[letter] },
		"exec": func(name string, page Page) (template.HTML, error) {
			var buf bytes.Buffer
			if err := templates.ExecuteTemplate(&buf, name, page); err != nil {
				return "", err
			}
			return template.HTML(buf.String()), nil
		},
		"Denorm": func(str string) string {
			return words.Denormalize(str)
		},
		"AgentMoji": func(agent string) string {
			switch strings.ToLower(agent) {
			case "human":
				return "👩"
			case "robot":
				return "💻"
			default:
				return " "
			}
		},
	}).ParseGlob("templates/*.html")
	if err != nil {
		panic(fmt.Sprintf("bad templates: %v", err))
	}
	if time.Local, err = time.LoadLocation("Asia/Tokyo"); err != nil {
		panic(err)
	}
	dict = mustLoadDict("filtered.words")
	initFreqs()
	if true {
		letterGen = words.NewLetterGenFreqs(invertPoints(pointVals))
	} else {
		letterGen = words.NewLetterGenCorpus(dict)
	}
	http.HandleFunc("/highscores", Handle("highscores", HighScores))
	http.HandleFunc("/highscore", Handle("highscores", HighScores))
	http.HandleFunc("/help", Handle("help", Help))
	http.HandleFunc("/", Handle("puzzle", Puzzle))
}

func initFreqs() map[string]int64 {
	base := map[int64]string{
		2: "lcfhmpvwy",
		3: "jkqxz",
	}
	pointVals = make(map[string]int64)
	// Default value 1.
	for c := 'a'; c <= 'z'; c++ {
		pointVals[strings.ToUpper(string(c))] = 1
	}
	// Increase the value of the special letters.
	for val, letters := range base {
		for _, l := range strings.Split(strings.ToUpper(letters), "") {
			pointVals[l] = val
		}
	}
	// Invert for easy reference
	pointLetters = make(map[string][]string)
	for l, v := range pointVals {
		str := fmt.Sprintf("p%d", v) // template fields can't start with a digit??
		pointLetters[str] = append(pointLetters[str], l)
	}
	// And sort
	for v := range pointLetters {
		sort.Sort(sort.StringSlice(pointLetters[v]))
	}
	return pointVals
}

func invertPoints(points map[string]int64) map[string]int64 {
	res := make(map[string]int64)
	for l, v := range points {
		res[l] = 1000 / v
	}
	return res
}

func Handle(name string, f func(context.Context, *http.Request, *Page) error) func(http.ResponseWriter, *http.Request) {
	templ := templates.Lookup(name + ".html")
	if templ == nil {
		panic("template not found: " + name)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		respError := func(code int, err error) {
			w.Header().Set("Content-Type", kPlain)
			w.WriteHeader(code)
			fmt.Fprint(w, err)
		}
		ctx := appengine.NewContext(r)
		err := r.ParseForm()
		if err != nil {
			log.Warningf(ctx, "form parse failed: %v", err)
			respError(418, err)
			return
		}

		var page Page
		page.Template = templ.Name()
		page.User = user.Current(ctx)
		if page.User == nil {
			page.LoginURL, err = user.LoginURL(ctx, "/")
		} else {
			page.LogoutURL, err = user.LogoutURL(ctx, "/")
			page.Admin = page.User.Admin
		}
		if err != nil {
			respError(418, err)
		}

		err = f(ctx, r, &page)
		if err != nil {
			log.Warningf(ctx, "request errored: %v", err)
			respError(500, err)
			return
		}
		var buf bytes.Buffer
		err = templates.ExecuteTemplate(&buf, "base.html", page)
		if err != nil {
			log.Criticalf(ctx, "template error: %v", err)
			respError(500, err)
			return
		}
		w.Header().Set("Content-Type", kHTML)
		fmt.Fprint(w, buf.String())
	}
}

type Game struct {
	Started time.Time `datastore:",noindex"`
	Seed    int64     `datastore:",noindex"`
	Moves   []string  `datastore:",noindex"`
	Letters []string  `datastore:"-"`
	Over    bool      `datastore:",noindex"`

	rng *rand.Rand
}

func randInt64() int64 {
	var i int64
	if err := binary.Read(crand.Reader, binary.LittleEndian, &i); err != nil {
		panic(err)
	}
	return i
}

func NewGame(ctx context.Context) *Game {
	var g Game
	log.Infof(ctx, "Starting new game")
	g.Seed = randInt64()
	g.Started = time.Now()
	g.Unwind(ctx, nil)
	return &g
}

func (g *Game) Unwind(ctx context.Context, moves []string) error {
	g.rng = rand.New(rand.NewSource(g.Seed ^ g.Started.Unix()))
	g.Draw()
	for i, move := range moves {
		if err := g.DoMove(ctx, move); err != nil {
			return fmt.Errorf("illegal move in unwind step #%v: %v", i, err)
		}
	}
	return nil
}

func (g *Game) Draw() {
	for len(g.Letters) < kSize {
		l := g.RandLetter()
		g.Letters = append(g.Letters, l)
	}
}

func (g *Game) RandLetter() string {
	cm := words.CountLetters(g.Letters)
	l := letterGen.Next(g.rng)
	for g.rng.Int63n(1+pointVals[l]*int64(cm[l])) > 0 {
		l = letterGen.Next(g.rng)
	}
	return l
	//	return string(g.rng.Intn(26) + 'A')
}

func (g Game) Board() (b [kLen][kLen]string) {
	for i := 0; i < len(g.Letters) && i < kSize; i++ {
		b[i/kLen][i%kLen] = g.Letters[i]
	}
	return b
}

func Puzzle(ctx context.Context, r *http.Request, p *Page) error {
	var err error
	if p.Game, err = ResumeGame(ctx, r); err != nil {
		return err
	}
	move := r.FormValue("move")
	if move == "" && r.FormValue("pass") != "PASS" {
		return nil
	}
	err = p.Game.DoMove(ctx, move)
	p.maybeNotify(err)
	return nil
}

// Tries to resume the game specified in the request.
func ResumeGame(ctx context.Context, r *http.Request) (*Game, error) {
	var g Game
	var err error
	if g.Seed, err = strconv.ParseInt(r.FormValue("Seed"), 0, 64); err != nil {
		return NewGame(ctx), nil
	}
	startUnix, err := strconv.ParseInt(r.FormValue("Started"), 0, 64)
	if err != nil {
		return NewGame(ctx), nil
	}
	g.Started = time.Unix(startUnix, 0)
	err = g.Unwind(ctx, r.Form["Moves"])
	return &g, err
}

func (g *Game) DoMove(ctx context.Context, move string) error {
	norm := words.Normalize(move)
	cm := words.Count(norm)
	if move == "" {
		g.Letters = nil
		g.Draw()
	} else {
		if !words.CountLetters(g.Letters).Contains(cm) {
			return fmt.Errorf("can't spell %v with %v", norm, words.CountLetters(g.Letters))
		}
		_, has := dict[norm]
		if !has {
			return fmt.Errorf("unknown word: %v", norm)
		}
	}
	g.Moves = append(g.Moves, norm)
	if len(g.Moves) >= kGameLen {
		g.Over = true
		log.Infof(ctx, "Game completed: %+v", g)
	}
	for i, l := range g.Letters {
		if cm[l] > 0 {
			g.Letters[i] = g.RandLetter()
			cm[l]--
		}
	}
	return nil
}

func (g *Game) Score() int64 {
	var score int64
	for _, move := range g.Moves {
		score += ScoreWord(move)
	}
	return score
}

func ScoreWord(word string) int64 {
	if word == "" {
		return 0
	}
	score := int64(1)
	norm := words.Normalize(word)
	for _, l := range strings.Split(norm, "") {
		score += pointVals[l]
	}
	return score * score
}

type HighScore struct {
	Score    int64
	Time     time.Time
	Duration time.Duration
	Game     Game   `datastore:",noindex"`
	NickName string `datastore:",noindex"`
	URL      string `datastore:",noindex"`
	Name     string `datastore:",noindex"`
	Agent    string `datastore:",noindex"`
	Email    string
}

func checkURL(URL string) bool {
	url, err := url.Parse(URL)
	if err != nil {
		return false
	}
	return schemeRE.MatchString(url.Scheme) && hostRE.MatchString(url.Host)
}

func HighScores(ctx context.Context, r *http.Request, p *Page) error {
	p.Title = "High Scores"
	game, err := ResumeGame(ctx, r)
	if err == nil && game.Over {
		hs := HighScore{
			Time:     time.Now(),
			NickName: r.FormValue("NickName"),
			Name:     r.FormValue("Name"),
			URL:      r.FormValue("URL"),
			Email:    r.FormValue("Email"),
			Agent:    r.FormValue("Agent"),
			Duration: time.Now().Sub(game.Started),
			Game:     *game,
			Score:    game.Score(),
		}
		err = writeHighScore(ctx, hs)
		p.maybeNotify(err)
		log.Infof(ctx, "highscore (%+v) written = %v", hs, err)
	}
	q := datastore.NewQuery(kHSKind).Ancestor(hsRoot(ctx)).Order("-Score")
	_, err = q.GetAll(ctx, &p.HighScores)
	if err != nil {
		return err
	}
	for i := range p.HighScores {
		hs := &p.HighScores[i]
		if !checkURL(hs.URL) {
			hs.URL = ""
		}
	}
	return err
}

func hsRoot(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, "HSRoot", "", 1, nil)
}

func (hs HighScore) Key(ctx context.Context) *datastore.Key {
	return datastore.NewKey(ctx, kHSKind, "", hs.Game.Seed, hsRoot(ctx))
}

func writeHighScore(ctx context.Context, hs HighScore) error {
	key := hs.Key(ctx)
	var prev HighScore
	err := datastore.Get(ctx, key, &prev)
	if err != nil && err != datastore.ErrNoSuchEntity {
		return err
	}
	if err == nil {
		return fmt.Errorf("HighScore for specified game has already been entered")
	}
	_, err = datastore.Put(ctx, key, &hs)
	return err
}

func Help(ctx context.Context, r *http.Request, p *Page) error {
	p.PointLetters = pointLetters
	return nil
}
