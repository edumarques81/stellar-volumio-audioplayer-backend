package socketio

import (
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/library"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/localmusic"
)

// LibraryClassifierAdapter adapts the localmusic.PathClassifier to the library.PathClassifier interface.
type LibraryClassifierAdapter struct {
	classifier *localmusic.PathClassifier
}

// NewLibraryClassifierAdapter creates a new adapter.
func NewLibraryClassifierAdapter(classifier *localmusic.PathClassifier) *LibraryClassifierAdapter {
	return &LibraryClassifierAdapter{classifier: classifier}
}

// GetSourceType returns the source type for a URI.
func (a *LibraryClassifierAdapter) GetSourceType(uri string) library.SourceType {
	sourceType := a.classifier.GetSourceType(uri)

	// Convert localmusic.SourceType to library.SourceType
	switch sourceType {
	case localmusic.SourceLocal:
		return library.SourceLocal
	case localmusic.SourceUSB:
		return library.SourceUSB
	case localmusic.SourceNAS:
		return library.SourceNAS
	case localmusic.SourceStreaming:
		return library.SourceStreaming
	default:
		return library.SourceLocal
	}
}
