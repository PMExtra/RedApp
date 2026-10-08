package main

import (
	"fmt"
	"github.com/PMExtra/RedApp/presets"
	"os"
)

func main() {
	data, err := presets.Inventory(presets.Embedded())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}
