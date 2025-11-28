//go:build run

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/goark/webinfo"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	info, err := webinfo.Fetch(ctx, "https://example.com", "")
	if err != nil {
		log.Fatalf("Fetch failed: %v", err)
	}
	fmt.Printf("Title: %s\nDescription: %s\nImage: %s\n", info.Title, info.Description, info.ImageURL)
}
