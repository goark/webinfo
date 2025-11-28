//go:build run

package main

import (
	"context"
	"fmt"

	"github.com/goark/webinfo"
)

func main() {
	ctx := context.Background()
	// Fetch metadata for a page (empty UA uses default)
	info, err := webinfo.Fetch(ctx, "https://text.baldanders.info/", "")
	if err != nil {
		fmt.Printf("%+v\n", err)
		return
	}

	// Download thumbnail: width 150, to directory "thumbnails", permanent file
	thumbPath, err := info.DownloadThumbnail(ctx, "thumbnails", 150, false)
	if err != nil {
		fmt.Printf("%+v\n", err)
		return
	}
	fmt.Println("thumbnail saved:", thumbPath)
}
