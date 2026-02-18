package main

import (
	"os"

	"sprout/internal/sprout"
)

func main() {
	os.Exit(sprout.Run(os.Args[1:]))
}
