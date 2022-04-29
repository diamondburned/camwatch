package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"time"
)

func main() {
	var (
		player = "mpv --cache=no"
		rate   = 15
	)

	flag.StringVar(&player, "player", player, "video player to use")
	flag.IntVar(&rate, "rate", rate, "polling rate per second")
	flag.Parse()

	url := flag.Arg(0)
	if url == "" {
		log.Fatalln("usage: camwatch <url>")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	sh := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf(
		"ffmpeg -loglevel warning -f image2pipe -r %d -vcodec mjpeg -i - -f matroska - | %s -",
		rate, player,
	))

	sh.Stdout = os.Stdout
	sh.Stderr = os.Stderr

	videoIn, err := sh.StdinPipe()
	must(err, "cannot make stdin pipe")

	defer videoIn.Close()

	must(sh.Start(), "cannot start ffmpeg pipeline")
	defer sh.Wait()

	clock := time.NewTicker(time.Second / 15)
	for {
		downloadFrame(ctx, videoIn, url)
		select {
		case <-ctx.Done():
			return
		case <-clock.C:
		}
	}
}

func downloadFrame(ctx context.Context, w io.Writer, url string) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	must(err, "cannot make request")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalln("cannot request video:", err)
		return
	}
	defer resp.Body.Close()

	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Println("cannot download video:", err)
		return
	}
}

func must(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %v", msg, err)
	}
}
