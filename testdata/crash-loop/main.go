package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	fmt.Println("Crash loop app starting...")
	fmt.Println("Running for 2 seconds before crashing...")
	time.Sleep(2 * time.Second)
	fmt.Println("Time's up! Crashing now!")
	os.Exit(1)
}
