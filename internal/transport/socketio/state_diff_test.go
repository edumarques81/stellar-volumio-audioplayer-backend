package socketio

import (
	"testing"
)

func TestStateCompareKeys_DoesNotIncludeSeek(t *testing.T) {
	// Seek is excluded from state diff keys because the frontend interpolates
	// seek client-side. Including seek causes unnecessary broadcasts when
	// seek is the only field that drifted since the last broadcast.
	for _, key := range stateCompareKeys {
		if key == "seek" {
			t.Error("stateCompareKeys should not include 'seek' — frontend interpolates seek client-side")
		}
	}
}

func TestIsStateSame_SeekOnlyChange_ReturnsTrue(t *testing.T) {
	// Create a minimal server with just the fields needed for diffing
	s := &Server{}

	// Set initial state
	baseState := map[string]interface{}{
		"status":   "play",
		"position": 0,
		"title":    "Test Song",
		"artist":   "Test Artist",
		"album":    "Test Album",
		"volume":   50,
		"duration": 300,
		"random":   false,
		"repeat":   false,
		"seek":     1000, // seek IS in the state, just not compared
	}
	s.saveLastState(baseState)

	// Change only seek — should be considered "same" since seek is not diffed
	seekOnlyChanged := map[string]interface{}{
		"status":   "play",
		"position": 0,
		"title":    "Test Song",
		"artist":   "Test Artist",
		"album":    "Test Album",
		"volume":   50,
		"duration": 300,
		"random":   false,
		"repeat":   false,
		"seek":     5000, // only seek changed
	}

	if !s.isStateSame(seekOnlyChanged) {
		t.Error("isStateSame should return true when only seek changed (seek is excluded from diff)")
	}
}

func TestIsStateSame_VolumeChange_ReturnsFalse(t *testing.T) {
	s := &Server{}

	baseState := map[string]interface{}{
		"status":   "play",
		"position": 0,
		"title":    "Test Song",
		"volume":   50,
		"duration": 300,
	}
	s.saveLastState(baseState)

	// Change volume — should be considered "different"
	volumeChanged := map[string]interface{}{
		"status":   "play",
		"position": 0,
		"title":    "Test Song",
		"volume":   75,
		"duration": 300,
	}

	if s.isStateSame(volumeChanged) {
		t.Error("isStateSame should return false when volume changed")
	}
}

func TestIsStateSame_TitleChange_ReturnsFalse(t *testing.T) {
	s := &Server{}

	baseState := map[string]interface{}{
		"status": "play",
		"title":  "Song A",
		"artist": "Artist",
		"volume": 50,
	}
	s.saveLastState(baseState)

	titleChanged := map[string]interface{}{
		"status": "play",
		"title":  "Song B",
		"artist": "Artist",
		"volume": 50,
	}

	if s.isStateSame(titleChanged) {
		t.Error("isStateSame should return false when title changed")
	}
}
