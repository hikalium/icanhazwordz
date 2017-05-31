package main

import (
	"flag"
	"fmt"
	"log"
	"words"
)

var (
	maxLen = flag.Int("max_length", 16, "max word length")
	exp    = flag.String("exp", "filter", "What experiment to do?")
)

func main() {
	flag.Parse()
	files := flag.Args()
	if len(files) < 1 {
		files = append(files, "/usr/share/dict/words")
	}
	log.Printf("Reading files: %v", files)
	ch := make(chan string, 32)
	go func() {
		for _, file := range files {
			log.Printf("Reading %v", file)
			words.LoadValidFile(file, *maxLen, ch)
		}
		close(ch)
	}()
	switch *exp {
	case "max":
		max := words.Count("")
		for word := range ch {
			max = words.Max(max, words.Count(word))
		}
		fmt.Printf("Max: %v (length=%v)\n", max, len(max.String()))
	case "filter":
		for word := range ch {
			fmt.Println(word)
		}
	}
}
