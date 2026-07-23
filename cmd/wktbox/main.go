package main

import (
	"fmt"

	"wktbox/internal/version"
)

func main() {
	fmt.Printf("wktbox %s\n", version.String())
}
