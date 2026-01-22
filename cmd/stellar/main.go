// Package main is the entry point for the Stellar audio player backend.
package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/player"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/transport/socketio"
)

const version = "0.1.0-dev"

func main() {
	// Command line flags
	port := flag.String("port", "3001", "HTTP server port")
	mpdHost := flag.String("mpd-host", "localhost", "MPD host")
	mpdPort := flag.Int("mpd-port", 6600, "MPD port")
	mpdPassword := flag.String("mpd-password", "", "MPD password")
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	}

	log.Info().
		Str("version", version).
		Str("port", *port).
		Str("mpd_host", *mpdHost).
		Int("mpd_port", *mpdPort).
		Msg("Starting Stellar Audio Player Backend")

	// Create MPD client
	mpdClient := mpd.NewClient(*mpdHost, *mpdPort, *mpdPassword)
	if err := mpdClient.Connect(); err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to MPD")
	}
	defer mpdClient.Close()

	// Verify MPD connection
	if err := mpdClient.Ping(); err != nil {
		log.Fatal().Err(err).Msg("MPD ping failed")
	}
	log.Info().Msg("MPD connection verified")

	// Create services
	playerService := player.NewService(mpdClient)

	// Create Socket.io server
	socketServer, err := socketio.NewServer(playerService, mpdClient)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create Socket.io server")
	}
	defer socketServer.Close()

	// Start MPD watcher
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := socketServer.StartMPDWatcher(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to start MPD watcher")
	}

	// Setup HTTP server
	mux := http.NewServeMux()

	// Socket.io endpoint
	mux.Handle("/socket.io/", socketServer)

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := mpdClient.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"error","mpd":"disconnected"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","mpd":"connected"}`))
	})

	// Version endpoint
	mux.HandleFunc("/api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":"` + version + `","name":"Stellar"}`))
	})

	// Album art endpoint
	mux.HandleFunc("/albumart", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "path parameter required", http.StatusBadRequest)
			return
		}

		// Try embedded picture first (ReadPicture)
		data, err := mpdClient.ReadPicture(path)
		if err != nil {
			// Fall back to album art file in directory (AlbumArt)
			data, err = mpdClient.AlbumArt(path)
			if err != nil {
				log.Debug().Err(err).Str("path", path).Msg("Album art not found")
				http.Error(w, "album art not found", http.StatusNotFound)
				return
			}
		}

		// Detect content type from image magic bytes
		contentType := "image/jpeg" // default
		if len(data) >= 8 {
			if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
				contentType = "image/png"
			} else if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 {
				contentType = "image/gif"
			} else if data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 {
				contentType = "image/webp"
			}
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=86400") // Cache for 1 day
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(data)
	})

	// Basic state endpoint (REST fallback)
	mux.HandleFunc("/api/v1/getState", func(w http.ResponseWriter, r *http.Request) {
		state, err := playerService.GetState()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		// Simple JSON encoding
		data := "{"
		first := true
		for k, v := range state {
			if !first {
				data += ","
			}
			first = false
			switch val := v.(type) {
			case string:
				data += `"` + k + `":"` + val + `"`
			case int:
				data += `"` + k + `":` + itoa(val)
			case bool:
				if val {
					data += `"` + k + `":true`
				} else {
					data += `"` + k + `":false`
				}
			default:
				data += `"` + k + `":null`
			}
		}
		data += "}"
		w.Write([]byte(data))
	})

	// Start HTTP server
	server := &http.Server{
		Addr:         ":" + *port,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		log.Info().Msg("Shutting down...")
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("Server shutdown error")
		}
	}()

	log.Info().Str("addr", ":"+*port).Msg("HTTP server listening")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("HTTP server error")
	}

	log.Info().Msg("Server stopped")
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
