package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
)

func main() {
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Println("atc dev")
		os.Exit(0)
	}

	http.ListenAndServe(":8765", nil)
}
