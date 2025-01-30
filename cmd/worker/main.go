package main

import (
	"context"
	"net/http"

	"github.com/slcjordan/autodemo/db"
	"github.com/slcjordan/autodemo/video"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clicks := video.NewKeyboardClicks(ctx, "/assets/sound-effects")
	conn, err := db.Open("/mytest/db.sqlite")
	conn.ApplySchema(ctx)
	if err != nil {
		panic(err)
	}
	w, err := video.NewWorker(ctx, conn, clicks, 99)
	if err != nil {
		panic(err)
	}
	go w.Run(ctx)

	http.ListenAndServe(":8080", video.NewAPI(conn))
}
