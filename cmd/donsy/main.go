package main

import (
	"fmt"
	"io"
	"os"
)

const description = "donsy is the go-merge daemon and host"

func main() {
	if err := printDescription(os.Stdout); err != nil {
		os.Exit(1)
	}
}

func printDescription(w io.Writer) error {
	_, err := fmt.Fprintln(w, description)
	return err
}
