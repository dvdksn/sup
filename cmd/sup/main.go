package main

import (
	"fmt"
	"os"

	"github.com/dvdksn/sup/internal/sup"
)

func main() {
	code, err := sup.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sup:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
