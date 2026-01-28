package mpd_test

import (
	"testing"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/mpd"
)

func TestNewClient(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	if client == nil {
		t.Error("NewClient should return a non-nil client")
	}
}

func TestClientConnectFailure(t *testing.T) {
	// Test connection to non-existent server
	client := mpd.NewClient("localhost", 16600, "") // Wrong port

	err := client.Connect()
	if err == nil {
		t.Error("Connect should fail for non-existent server")
		client.Close()
	}
}

func TestClientPingWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Ping()
	if err == nil {
		t.Error("Ping should fail when not connected")
	}
}

func TestClientStatusWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.Status()
	if err == nil {
		t.Error("Status should fail when not connected")
	}
}

func TestClientPlayWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Play(0)
	if err == nil {
		t.Error("Play should fail when not connected")
	}
}

func TestClientPauseWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Pause(true)
	if err == nil {
		t.Error("Pause should fail when not connected")
	}
}

func TestClientStopWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Stop()
	if err == nil {
		t.Error("Stop should fail when not connected")
	}
}

func TestClientNextWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Next()
	if err == nil {
		t.Error("Next should fail when not connected")
	}
}

func TestClientPreviousWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Previous()
	if err == nil {
		t.Error("Previous should fail when not connected")
	}
}

func TestClientSetVolumeWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.SetVolume(50)
	if err == nil {
		t.Error("SetVolume should fail when not connected")
	}
}

func TestClientSetRandomWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.SetRandom(true)
	if err == nil {
		t.Error("SetRandom should fail when not connected")
	}
}

func TestClientSetRepeatWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.SetRepeat(true)
	if err == nil {
		t.Error("SetRepeat should fail when not connected")
	}
}

func TestClientPlaylistInfoWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.PlaylistInfo()
	if err == nil {
		t.Error("PlaylistInfo should fail when not connected")
	}
}

func TestClientClearWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Clear()
	if err == nil {
		t.Error("Clear should fail when not connected")
	}
}

func TestClientAddWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Add("test.flac")
	if err == nil {
		t.Error("Add should fail when not connected")
	}
}

func TestClientDetectCapabilitiesWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.DetectCapabilities()
	if err == nil {
		t.Error("DetectCapabilities should fail when not connected")
	}
}

func TestClientWatchDatabaseWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.WatchDatabase()
	if err == nil {
		t.Error("WatchDatabase should fail when not connected")
	}
}

func TestCapabilityFlagsDefaults(t *testing.T) {
	// Test that default CapabilityFlags has false values
	flags := mpd.CapabilityFlags{}

	if flags.HasReadPicture {
		t.Error("Default HasReadPicture should be false")
	}
	if flags.HasAlbumArt {
		t.Error("Default HasAlbumArt should be false")
	}
	if flags.ProtocolVersion != "" {
		t.Error("Default ProtocolVersion should be empty")
	}
}

func TestClientGetDatabaseStatsWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.GetDatabaseStats()
	if err == nil {
		t.Error("GetDatabaseStats should fail when not connected")
	}
}

func TestClientCountAlbumsWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.CountAlbums()
	if err == nil {
		t.Error("CountAlbums should fail when not connected")
	}
}

func TestClientCountArtistsWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.CountArtists()
	if err == nil {
		t.Error("CountArtists should fail when not connected")
	}
}

// Tests for Volumio integration queue manipulation methods

func TestClientAddIdWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.AddId("test.flac", -1)
	if err == nil {
		t.Error("AddId should fail when not connected")
	}
}

func TestClientAddIdAtPositionWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.AddId("test.flac", 0)
	if err == nil {
		t.Error("AddId at position should fail when not connected")
	}
}

func TestClientMoveWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Move(0, 1)
	if err == nil {
		t.Error("Move should fail when not connected")
	}
}

func TestClientDeleteWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	err := client.Delete(0)
	if err == nil {
		t.Error("Delete should fail when not connected")
	}
}

func TestClientGetCurrentPositionWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.GetCurrentPosition()
	if err == nil {
		t.Error("GetCurrentPosition should fail when not connected")
	}
}

func TestClientGetQueueLengthWithoutConnect(t *testing.T) {
	client := mpd.NewClient("localhost", 6600, "")

	_, err := client.GetQueueLength()
	if err == nil {
		t.Error("GetQueueLength should fail when not connected")
	}
}
