package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/corvines/outrider/internal/chat"
)

func main() {
	endpoint := flag.String("endpoint", "", "model endpoint")
	flag.Parse()

	if err := chat.Run(*endpoint); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
