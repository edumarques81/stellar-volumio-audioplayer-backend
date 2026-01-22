// Package main is the entry point for the Stellar audio player backend.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/player"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/transport/socketio"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/version"
)

func main() {
	// Command line flags
	port := flag.String("port", "3001", "HTTP server port")
	mpdHost := flag.String("mpd-host", "localhost", "MPD host")
	mpdPort := flag.Int("mpd-port", 6600, "MPD port")
	mpdPassword := flag.String("mpd-password", "", "MPD password")
	exclusive := flag.Bool("exclusive", false, "Enable exclusive MPD access mode (requires password, blocks other clients)")
	bitPerfect := flag.Bool("bit-perfect", true, "Enable bit-perfect audio mode (default true)")
	staticDir := flag.String("static", "", "Directory to serve static files from (optional)")
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	// Warn if exclusive mode is enabled without password
	if *exclusive && *mpdPassword == "" {
		log.Warn().Msg("Exclusive mode enabled but no MPD password set - MPD may still accept other connections")
	}

	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	}

	// Print startup banner
	versionInfo := version.GetInfo()
	log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Info().Msgf("  %s", versionInfo.String())
	log.Info().Msg("  Bit-Perfect Audio Player Backend")
	log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Info().
		Str("port", *port).
		Str("mpd_host", *mpdHost).
		Int("mpd_port", *mpdPort).
		Bool("exclusive", *exclusive).
		Bool("bit_perfect", *bitPerfect).
		Bool("password_set", *mpdPassword != "").
		Msg("Configuration")

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
	socketServer, err := socketio.NewServer(playerService, mpdClient, *bitPerfect)
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

	// Start network watcher for Socket.IO push notifications
	socketServer.StartNetworkWatcher(ctx)

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
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(version.GetInfo())
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

	// Network status endpoint
	mux.HandleFunc("/api/v1/network", func(w http.ResponseWriter, r *http.Request) {
		status := getNetworkStatus()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(status)
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

	// Serve static files if directory specified (SPA mode)
	if *staticDir != "" {
		log.Info().Str("dir", *staticDir).Msg("Serving static files")
		fs := http.FileServer(http.Dir(*staticDir))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// Check if the file exists
			path := *staticDir + r.URL.Path
			if r.URL.Path == "/" {
				path = *staticDir + "/index.html"
			}
			if _, err := os.Stat(path); os.IsNotExist(err) {
				// For SPA routing, serve index.html for non-existing paths
				http.ServeFile(w, r, *staticDir+"/index.html")
				return
			}
			fs.ServeHTTP(w, r)
		})
	}

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

// NetworkStatus represents the current network connection status
type NetworkStatus struct {
	Type     string `json:"type"`     // "wifi", "ethernet", "none"
	SSID     string `json:"ssid"`     // WiFi network name (if wifi)
	Signal   int    `json:"signal"`   // WiFi signal strength 0-100 (if wifi)
	IP       string `json:"ip"`       // IP address
	Strength int    `json:"strength"` // Signal strength level 0-3 (for icon)
}

// getNetworkStatus returns the current network connection status
func getNetworkStatus() NetworkStatus {
	status := NetworkStatus{
		Type:     "none",
		Signal:   0,
		Strength: 0,
	}

	// Check ethernet first (usually eth0 or end0 on newer Pi)
	for _, iface := range []string{"eth0", "end0"} {
		carrierPath := "/sys/class/net/" + iface + "/carrier"
		if data, err := os.ReadFile(carrierPath); err == nil {
			if strings.TrimSpace(string(data)) == "1" {
				status.Type = "ethernet"
				status.IP = getIPAddress(iface)
				status.Signal = 100
				status.Strength = 3
				return status
			}
		}
	}

	// Check WiFi (usually wlan0)
	for _, iface := range []string{"wlan0", "wlan1"} {
		operstatePath := "/sys/class/net/" + iface + "/operstate"
		if data, err := os.ReadFile(operstatePath); err == nil {
			if strings.TrimSpace(string(data)) == "up" {
				status.Type = "wifi"
				status.IP = getIPAddress(iface)
				status.SSID, status.Signal = getWifiInfo(iface)
				// Convert signal to strength level (0-3)
				switch {
				case status.Signal >= 70:
					status.Strength = 3 // Full signal
				case status.Signal >= 50:
					status.Strength = 2 // Medium
				case status.Signal >= 30:
					status.Strength = 1 // Weak
				default:
					status.Strength = 0 // Very weak
				}
				return status
			}
		}
	}

	return status
}

// getIPAddress returns the IP address for a given interface
func getIPAddress(iface string) string {
	out, err := exec.Command("ip", "-4", "addr", "show", iface).Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inet ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				// Remove CIDR notation
				ip := strings.Split(parts[1], "/")[0]
				return ip
			}
		}
	}
	return ""
}

// getWifiInfo returns SSID and signal strength (0-100) for a WiFi interface
func getWifiInfo(iface string) (string, int) {
	ssid := ""
	signal := 0

	// Get SSID using iwgetid
	out, err := exec.Command("iwgetid", iface, "-r").Output()
	if err == nil {
		ssid = strings.TrimSpace(string(out))
	}

	// Get signal from /proc/net/wireless
	file, err := os.Open("/proc/net/wireless")
	if err != nil {
		return ssid, signal
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, iface) {
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				// Signal level is in field 3 (link quality) or field 4 (signal level)
				// Format is usually: interface: status link level noise
				// Signal level is typically in dBm (negative) or link quality (0-100)
				linkQuality := strings.TrimSuffix(fields[2], ".")
				if q, err := strconv.Atoi(linkQuality); err == nil {
					// If it's a percentage (0-100), use directly
					if q >= 0 && q <= 100 {
						signal = q
					} else if q >= 0 && q <= 70 {
						// It's likely link quality out of 70
						signal = (q * 100) / 70
					}
				}

				// Also try signal level in dBm (field 3)
				if signal == 0 && len(fields) >= 4 {
					sigLevel := strings.TrimSuffix(fields[3], ".")
					if dbm, err := strconv.Atoi(sigLevel); err == nil {
						// Convert dBm to percentage (-100 dBm = 0%, -50 dBm = 100%)
						if dbm < 0 {
							signal = 2 * (dbm + 100)
							if signal < 0 {
								signal = 0
							}
							if signal > 100 {
								signal = 100
							}
						}
					}
				}
			}
			break
		}
	}

	return ssid, signal
}
