package main

import (
	"log"
	"os"

	"github.com/dantin/gnetutils/tools"
)

func main() {
	app := tools.NewEchoServer(os.Args)
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
