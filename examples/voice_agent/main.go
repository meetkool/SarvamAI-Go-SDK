package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/coder/websocket"
	sarvam "github.com/meetkool/SarvamAI-Go-SDK/src"
)

//go:embed web
var webFS embed.FS

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	voice := flag.String("voice", "shubh", "bulbul speaker name")
	useTLS := flag.Bool("tls", false, "serve HTTPS with a self-signed certificate, needed to reach the mic from another device")
	certDir := flag.String("certs", "certs", "directory holding the self-signed certificate")
	flag.Parse()

	loadDotEnv(".env")

	client, err := sarvam.NewFromEnv(sarvam.WithMetrics(func(m sarvam.Metric) {
		if m.Name == sarvam.MetricFirstAudio || m.Name == sarvam.MetricFirstToken {
			log.Printf("%s %v", m.Name, m.Duration.Round(time.Millisecond))
		}
	}))
	if err != nil {
		log.Fatal(err)
	}

	pages, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(pages)))
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			log.Println("accept:", err)
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(4 << 20)

		s := &session{client: client, conn: conn, voice: *voice}
		if err := s.run(r.Context()); err != nil {
			log.Println("session ended:", err)
		}
	})

	srv := &http.Server{Addr: *addr, Handler: mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	if *useTLS {
		for _, url := range serverURLs(*addr) {
			log.Println("open", url)
		}
		if err := serveTLS(srv, *certDir); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
		return
	}

	log.Printf("open http://localhost%s", *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
