// Package main is the entry point for the Stellar audio player backend.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	gompd "github.com/fhs/gompd/v2/mpd"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/airplay"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/artwork"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/bios"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/lastplayed"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/localmusic"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/player"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/sources"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/upnp"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/lcd"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/llm"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/netinfo"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/paths"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/spectrum"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/wikipedia"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/transport/mdns"
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

	// Create sources service for NAS/USB management
	sourcesConfigPath := filepath.Join(paths.DataDir(), "sources.json")
	sourcesService, err := sources.NewService(sourcesConfigPath, sources.NewPlatformMounter())
	if err != nil {
		log.Warn().Err(err).Msg("Failed to create sources service - NAS/USB management disabled")
		sourcesService = nil
	} else {
		// Set up NAS discoverer for Phase 2 discovery functionality
		sourcesService.SetDiscoverer(sources.NewPlatformDiscoverer())
		log.Info().Str("config", sourcesConfigPath).Msg("Sources service initialized with NAS discovery")

		// Auto-mount all configured NAS shares on startup. The 2-minute
		// budget bounds the entire pass; per-share mount syscalls get a
		// 30s derived deadline internally so one dead host cannot stall
		// the loop.
		startupMountCtx, startupMountCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		mountResults := sourcesService.MountAllShares(startupMountCtx)
		startupMountCancel()
		mountedCount := 0
		unmountedCount := 0
		for _, result := range mountResults {
			if result.Mounted {
				mountedCount++
				log.Info().
					Str("name", result.ShareName).
					Str("message", result.Message).
					Msg("NAS share ready")
			} else {
				unmountedCount++
				log.Warn().
					Str("name", result.ShareName).
					Str("error", result.Error).
					Msg("Failed to mount NAS share")
			}
		}
		if len(mountResults) > 0 {
			log.Info().
				Int("total", len(mountResults)).
				Int("mounted", mountedCount).
				Msg("NAS shares initialization complete")
		}

		// Retry unmounted shares in background with exponential backoff
		if unmountedCount > 0 {
			svc := sourcesService // capture for goroutine
			mpdC := mpdClient
			go func() {
				retryCtx, retryCancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer retryCancel()
				results := svc.MountAllSharesWithRetry(retryCtx, 5, 5*time.Second)
				finalMounted := 0
				for _, r := range results {
					if r.Mounted {
						finalMounted++
					}
				}
				if finalMounted > mountedCount {
					log.Info().Int("mounted", finalMounted).Msg("NAS shares mounted after retry, triggering MPD update")
					mpdC.Update("")
				}
			}()
		}
	}

	// Create local music service for local-only browsing and history
	localMusicDataDir := paths.DataDir()
	mpdMusicDir := "/var/lib/mpd/music"
	mpdAdapter := &mpdClientAdapter{client: mpdClient}
	localMusicService := localmusic.NewService(mpdAdapter, localMusicDataDir, mpdMusicDir)
	log.Info().
		Str("dataDir", localMusicDataDir).
		Str("mpdMusicDir", mpdMusicDir).
		Msg("Local music service initialized")

	// Create Socket.io server
	socketServer, err := socketio.NewServer(playerService, mpdClient, sourcesService, localMusicService, *bitPerfect)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create Socket.io server")
	}
	defer socketServer.Close()

	// Wire platform-selected LCD controller (real impl on Linux/Pi, stubs
	// on darwin/windows). Must run before the HTTP server starts accepting
	// connections — the connection callback reads s.lcdController without
	// locking.
	socketServer.SetLCDController(lcd.NewPlatform())

	// Wire platform-selected network Reporter (real impls on Linux/darwin,
	// stub on windows). Must run before the HTTP server accepts connections —
	// the connection callback reads s.netReporter without locking.
	netReporter := netinfo.NewPlatform()
	socketServer.SetNetReporter(netReporter)

	// Initialize library cache (triggers background build if empty)
	socketServer.InitializeCache()

	// Bio pipeline: Wikipedia client → LLM client → bio service → handlers.
	// Skipped silently if the cache DB failed to open in NewServer.
	if cacheDB := socketServer.CacheDB(); cacheDB != nil {
		wikiClient := wikipedia.NewClient(wikipedia.Config{}) // defaults are fine
		var llmClient llm.Client
		if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
			model := os.Getenv("ANTHROPIC_MODEL")
			if model == "" {
				model = "claude-haiku-4-5-20251001"
			}
			llmClient = llm.NewAnthropic(apiKey, model)
			log.Info().Str("model", model).Msg("Bio LLM provider: Anthropic")
		} else {
			llmClient = llm.NewNoop()
			log.Warn().Msg("ANTHROPIC_API_KEY not set; bios will be empty (using noop LLM)")
		}
		biosSvc := bios.NewService(cacheDB.BiosDAO(), wikiClient, llmClient, bios.Config{}) // 90-day TTL default
		socketServer.SetBioHandlers(socketio.NewBioHandlers(biosSvc))
		log.Info().Msg("Bio service registered (library:bio:get / library:bio:rebuild)")

		// Last-played album service shares the cache DB so resume state
		// survives backend restarts.
		lastPlayedSvc := lastplayed.NewService(cacheDB.LastPlayedDAO())
		socketServer.SetLastPlayed(lastPlayedSvc)
		log.Info().Msg("Last-played service registered (library:lastPlayed:get)")
	} else {
		log.Warn().Msg("Cache DB unavailable; bio + last-played services disabled")
	}

	// Register `shutdown` / `reboot` socket handlers. Loopback callers are
	// always authorized; STELLAR_POWER_TRUSTED_REMOTES (CSV of IPs/CIDRs)
	// extends the gate to LAN clients (kiosk + dev frontend). In the M1.C+
	// Mac/Windows topology, STELLAR_MOUNT_REMOTE_URL+_TOKEN being set tells
	// us to route the action to the Pi mount-control service
	// (/api/system/*) so it lands on the audio appliance instead of the
	// backend host. On Linux (Pi-resident) those env vars are unset and
	// DefaultShutdown/DefaultReboot exec /sbin/shutdown locally.
	trustedRemotes := strings.Split(os.Getenv("STELLAR_POWER_TRUSTED_REMOTES"), ",")
	sysDeps := socketio.SystemActionDeps{
		Broadcast: socketServer.Broadcaster().Emit,
	}
	if mountURL := os.Getenv("STELLAR_MOUNT_REMOTE_URL"); mountURL != "" {
		if mountTok := os.Getenv("STELLAR_MOUNT_REMOTE_TOKEN"); mountTok != "" {
			rsa := socketio.NewRemoteSystemActions(mountURL, mountTok)
			sysDeps.Shutdown = rsa.Shutdown
			sysDeps.Reboot = rsa.Reboot
			log.Info().Str("base_url", mountURL).Msg("System actions routed to Pi via mount-control /api/system/*")

			// M1.E read-handler proxy: route six READ socket handlers
			// (System/Device/Network info + Audio bit-perfect/DSD/mixer) to the
			// Pi appliance so the Settings page reflects Pi state instead of
			// Mac-host state. See internal/transport/socketio/remote_audio.go.
			ric := socketio.NewRemoteAudioClient(mountURL, mountTok)
			socketServer.UseRemoteAudio(ric)
			log.Info().Str("base_url", mountURL).Msg("Remote audio client wired (M1.E.1)")

			// M1.E.2 read-handler proxy: branch `getListNasShares` through
			// the Pi mount-control's /api/sources/list. Without this, Mac
			// stellar reports an empty share list on first run because its
			// local sources.json is empty post-cutover. Pi remains source
			// of truth for credentials + mount state.
			rsc := socketio.NewRemoteSourcesClient(mountURL, mountTok)
			socketServer.UseRemoteSources(rsc)
			log.Info().Str("base_url", mountURL).Msg("Remote sources client wired (M1.E.2)")
		}
	}
	systemActionHandlers, err := socketio.NewSystemActionHandlersWithTrusted(sysDeps, trustedRemotes)
	if err != nil {
		log.Fatal().Err(err).Msg("Invalid STELLAR_POWER_TRUSTED_REMOTES")
	}
	socketServer.SetSystemActionHandlers(systemActionHandlers)
	log.Info().Strs("trusted_remotes", trustedRemotes).Bool("remote_exec", sysDeps.Shutdown != nil).Msg("System action handlers registered (shutdown / reboot)")

	// Start MPD watcher
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := socketServer.StartMPDWatcher(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to start MPD watcher")
	}

	// Start network watcher for Socket.IO push notifications.
	// Skip the 30s netinfo watcher when running in remote mode. The Pi's
	// network rarely changes during a kiosk session on Ethernet, and the
	// frontend pulls /api/network/status on Settings tab mount + reconnect.
	// Polling the Pi every 30s would waste a HTTP round-trip per session.
	if os.Getenv("STELLAR_MOUNT_REMOTE_URL") == "" || os.Getenv("STELLAR_MOUNT_REMOTE_TOKEN") == "" {
		netinfo.StartWatcher(ctx, netReporter, socketServer.Broadcaster())
	} else {
		log.Info().Msg("Network watcher skipped (remote mode active — frontend pulls on demand)")
	}

	// Start mount watcher for periodic NAS share re-mount
	socketServer.StartMountWatcher(ctx)

	// Audirvana now-playing poller disabled — see plan the-pi-will-never-resilient-hartmanis
	// socketServer.StartAudirvanaPoller(ctx)

	// Spectrum source selection (M1.B):
	//   STELLAR_SPECTRUM_SOURCE=local  → read the MPD FIFO in-process (Pi-only
	//                                    deploy, legacy / dev path).
	//   STELLAR_SPECTRUM_SOURCE=remote → don't open the FIFO; instead expose
	//                                    /internal/spectrum so the Pi-side
	//                                    stellar-spectrum daemon can POST
	//                                    pre-computed frames.
	//   STELLAR_SPECTRUM_SOURCE=""     → default to "local" during M1.A/B
	//                                    (cutover flips this to "remote" in
	//                                    M1.C; tracked in the plan).
	spectrumSource := strings.ToLower(strings.TrimSpace(os.Getenv("STELLAR_SPECTRUM_SOURCE")))
	if spectrumSource == "" {
		spectrumSource = "local"
	}

	if spectrumSource == "local" {
		spectrumStreamer := spectrum.New(spectrum.Config{
			FIFOPath:   "/tmp/mpd_spectrum.fifo",
			SampleRate: 44100,
			FFTSize:    2048,
			NumBins:    64,
			FPS:        20, // CAVA reference rate; was 30 pre-M1.B
		})
		spectrumStreamer.Start(ctx, &socketIOEmitter{server: socketServer})
		defer spectrumStreamer.Stop()
		log.Info().Msg("Spectrum source: local FIFO (/tmp/mpd_spectrum.fifo @ 20 fps)")
	} else {
		log.Info().Str("source", spectrumSource).Msg("Spectrum source: remote (daemon will POST to /internal/spectrum)")
	}

	// Start UPnP AV Renderer for Audirvana discovery
	upnpService := upnp.NewService(
		upnp.DeviceConfig{
			FriendlyName: "Stellar Audio",
			UDN:          "4d696e69-4469-5343-9a61-5374656c6c61", // Stable UUID for Stellar
			Port:         9180,
		},
		&upnpBridge{player: playerService, mpd: mpdClient},
	)
	if err := upnpService.Start(); err != nil {
		log.Warn().Err(err).Msg("UPnP AV Renderer failed to start — Audirvana discovery disabled")
	} else {
		defer upnpService.Stop()
	}

	// Setup HTTP server
	mux := http.NewServeMux()

	// Socket.io endpoint
	mux.Handle("/socket.io/", socketServer)

	// Spectrum ingest endpoint (M1.B):
	// The Pi-side stellar-spectrum daemon POSTs computed frames here when
	// the backend runs on a different host than MPD. Authenticated via
	// STELLAR_SPECTRUM_KEY (shared bearer token); an empty key disables
	// the endpoint entirely with a 503 to prevent open-relay misconfig.
	spectrumKey := strings.TrimSpace(os.Getenv("STELLAR_SPECTRUM_KEY"))
	mux.Handle("/internal/spectrum", socketServer.SpectrumIngestHandler(spectrumKey))
	if spectrumKey != "" {
		log.Info().Str("route", "/internal/spectrum").Msg("Spectrum ingest endpoint enabled")
	} else {
		log.Info().Msg("Spectrum ingest endpoint disabled (STELLAR_SPECTRUM_KEY unset)")
	}

	// AirPlay ingest endpoints:
	// The Pi-side stellar-airplay daemon tails the shairport-sync metadata
	// pipe and POSTs delta payloads to /internal/airplay/state and pings
	// /internal/airplay/heartbeat. Auth via STELLAR_AIRPLAY_KEY; empty key
	// disables the endpoints with 503.
	airplayKey := strings.TrimSpace(os.Getenv("STELLAR_AIRPLAY_KEY"))
	airplayTimeout := 5 * time.Second
	if v := os.Getenv("STELLAR_AIRPLAY_SESSION_TIMEOUT_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			airplayTimeout = time.Duration(n) * time.Millisecond
		}
	}
	airplaySession := airplay.NewSession(airplay.SessionConfig{HeartbeatTimeout: airplayTimeout})
	mux.Handle("/internal/airplay/state", socketServer.AirplayIngestHandler(airplayKey, airplaySession))
	mux.Handle("/internal/airplay/heartbeat", socketServer.AirplayHeartbeatHandler(airplayKey, airplaySession))
	if airplayKey != "" {
		log.Info().
			Str("route", "/internal/airplay/{state,heartbeat}").
			Dur("session_timeout", airplayTimeout).
			Msg("AirPlay ingest enabled")

		// Wire the airplay:command socket handler. DACP resolver uses
		// dns-sd on macOS, avahi-browse on Linux; the DACPClient gets a
		// 2s timeout-bounded HTTP client.
		dacpResolver := airplay.NewDACPResolver()
		dacpClient := airplay.NewDACPClient(&http.Client{Timeout: 2 * time.Second})
		socketServer.UseAirplay(airplaySession, dacpClient, dacpResolver)

		// Heartbeat watcher: ticks every second, expires sessions whose
		// last heartbeat is older than airplayTimeout, broadcasts
		// pushAirplayEnded so clients flip out of AirPlay mode.
		socketio.StartAirplayHeartbeatWatcher(ctx, airplaySession, socketServer.Broadcaster().Emit, time.Second)
	} else {
		log.Info().Msg("AirPlay ingest disabled (STELLAR_AIRPLAY_KEY unset)")
	}

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
		json.NewEncoder(w).Encode(version.GetInfo())
	})

	// Create filesystem artwork finder
	filesystemFinder := artwork.NewFilesystemFinder(mpdMusicDir)

	// Album art endpoint
	mux.HandleFunc("/albumart", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "path parameter required", http.StatusBadRequest)
			return
		}

		var data []byte
		var err error

		// 1. Try filesystem search first (various filenames, parent dirs)
		artPath, fsErr := filesystemFinder.FindArtwork(path)
		if fsErr == nil && artPath != "" {
			data, err = filesystemFinder.ReadArtwork(artPath)
			if err == nil && len(data) > 0 {
				log.Debug().Str("path", path).Str("artPath", artPath).Msg("Serving artwork from filesystem")
				serveArtwork(w, data)
				return
			}
		}

		// 2. Try enrichment cache (works even when NAS is offline)
		if cachedData, mimeType := socketServer.GetAlbumArtworkByTrackPath(path); cachedData != nil {
			w.Header().Set("Content-Type", mimeType)
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.Write(cachedData)
			return
		}

		// 3. Try MPD albumart (folder-based) - fallback for remote sources
		data, err = mpdClient.AlbumArt(path)
		if err == nil && len(data) > 0 {
			log.Debug().Str("path", path).Msg("Serving artwork from MPD albumart")
			serveArtwork(w, data)
			return
		}

		// 4. Try embedded picture (ReadPicture)
		data, err = mpdClient.ReadPicture(path)
		if err == nil && len(data) > 0 {
			log.Debug().Str("path", path).Msg("Serving artwork from embedded picture")
			serveArtwork(w, data)
			return
		}

		log.Debug().Str("path", path).Msg("Album art not found")
		http.Error(w, "album art not found", http.StatusNotFound)
	})

	// Artist art endpoint - serves artist images from cache or redirects to external URLs
	mux.HandleFunc("/artistart", func(w http.ResponseWriter, r *http.Request) {
		artistID := r.URL.Query().Get("id")
		artistName := r.URL.Query().Get("name")

		if artistID == "" && artistName == "" {
			http.Error(w, "id or name parameter required", http.StatusBadRequest)
			return
		}

		// Try to get artist artwork from the cache via socket server
		artworkInfo := socketServer.GetArtistArtwork(artistID, artistName)
		if artworkInfo == nil {
			http.Error(w, "artist artwork not found", http.StatusNotFound)
			return
		}

		// If it's a URL (Deezer hotlink or album art URL), redirect to it
		if artworkInfo.IsURL {
			http.Redirect(w, r, artworkInfo.URL, http.StatusFound)
			return
		}

		// Serve from cached file
		if artworkInfo.Data != nil {
			serveArtwork(w, artworkInfo.Data)
			return
		}

		http.Error(w, "artist artwork not found", http.StatusNotFound)
	})

	// Network status endpoint
	mux.HandleFunc("/api/v1/network", func(w http.ResponseWriter, _ *http.Request) {
		status := netReporter.GetStatus()
		w.Header().Set("Content-Type", "application/json")
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
		Handler:      corsMiddleware(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Bind the listener up front so we can start the mDNS advertiser only
	// once we know the port is real. Using net.Listen + server.Serve
	// instead of ListenAndServe gives us that ordering guarantee.
	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		log.Fatal().Err(err).Str("addr", server.Addr).Msg("HTTP listen failed")
	}

	// Service discovery: advertise _stellar._tcp over Bonjour/mDNS so LAN
	// clients (iOS app, Volumio2-UI dev kiosks) can auto-connect without
	// hardcoded IPs. The advertised port matches whatever the HTTP server
	// bound — if --port was overridden, mDNS reflects that automatically.
	// Failures are non-fatal: the backend still serves clients that know
	// its address.
	var advertiser *mdns.BonjourAdvertiser
	if portInt, perr := strconv.Atoi(*port); perr == nil && portInt > 0 {
		adv, aerr := mdns.New(mdns.Config{Port: portInt})
		if aerr != nil {
			log.Warn().Err(aerr).Msg("mDNS advertiser construction failed - service discovery disabled")
		} else if serr := adv.Start(ctx); serr != nil {
			log.Warn().Err(serr).Msg("mDNS advertiser failed to start - service discovery disabled")
		} else {
			advertiser = adv
			log.Info().
				Str("service", "_stellar._tcp").
				Int("port", portInt).
				Msg("mDNS advertiser started")
		}
	} else {
		log.Warn().Str("port", *port).Msg("non-numeric port; mDNS advertiser disabled")
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		log.Info().Msg("Shutting down...")
		cancel()

		if advertiser != nil {
			if err := advertiser.Stop(); err != nil {
				log.Warn().Err(err).Msg("mDNS advertiser stop error")
			}
		}

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("Server shutdown error")
		}
	}()

	log.Info().Str("addr", ":"+*port).Msg("HTTP server listening")
	if err := server.Serve(ln); err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("HTTP server error")
	}

	log.Info().Msg("Server stopped")
}

// serveArtwork writes artwork data to the HTTP response with appropriate headers.
func serveArtwork(w http.ResponseWriter, data []byte) {
	contentType := artwork.DetectMimeType(data)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400") // Cache for 1 day
	w.Write(data)
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

// mpdClientAdapter adapts the MPD client to the localmusic.MPDClient interface.
// This is needed because gompd uses mpd.Attrs (a type alias) instead of map[string]string.
type mpdClientAdapter struct {
	client *mpd.Client
}

func (a *mpdClientAdapter) ListInfo(uri string) ([]map[string]string, error) {
	attrs, err := a.client.ListInfo(uri)
	if err != nil {
		return nil, err
	}
	return attrsToMaps(attrs), nil
}

func (a *mpdClientAdapter) ListAllInfo(uri string) ([]map[string]string, error) {
	attrs, err := a.client.ListAllInfo(uri)
	if err != nil {
		return nil, err
	}
	return attrsToMaps(attrs), nil
}

func (a *mpdClientAdapter) GetAlbumDetails(basePath string) ([]localmusic.AlbumDetails, error) {
	details, err := a.client.GetAlbumDetails(basePath)
	if err != nil {
		return nil, err
	}

	// Convert mpd.AlbumDetails to localmusic.AlbumDetails
	result := make([]localmusic.AlbumDetails, len(details))
	for i, d := range details {
		result[i] = localmusic.AlbumDetails{
			Album:       d.Album,
			AlbumArtist: d.AlbumArtist,
			TrackCount:  d.TrackCount,
			FirstTrack:  d.FirstTrack,
			TotalTime:   d.TotalTime,
		}
	}
	return result, nil
}

// attrsToMaps converts gompd Attrs slice to map slice.
func attrsToMaps(attrs []gompd.Attrs) []map[string]string {
	result := make([]map[string]string, len(attrs))
	for i, attr := range attrs {
		m := make(map[string]string, len(attr))
		for k, v := range attr {
			m[k] = v
		}
		result[i] = m
	}
	return result
}
