package socketio

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/bios"
)

type fakeBioService struct {
	getCalled     int
	refreshCalled int
	out           bios.Bio
	err           error
}

func (f *fakeBioService) GetAlbumBio(_ context.Context, _, _ string) (bios.Bio, error) {
	f.getCalled++
	return f.out, f.err
}

func (f *fakeBioService) RefreshAlbumBio(_ context.Context, _, _ string) (bios.Bio, error) {
	f.refreshCalled++
	return f.out, f.err
}

func TestBioHandlers_handleGetBio_HappyPath(t *testing.T) {
	t.Parallel()
	svc := &fakeBioService{
		out: bios.Bio{Summary: "Two-sentence summary.", SourceURL: "https://w/ko", Kind: "album"},
	}
	h := NewBioHandlers(svc)

	resp, err := h.handleGetBioInternal(map[string]interface{}{"artist": "Miles Davis", "album": "Kind of Blue"})
	if err != nil {
		t.Fatalf("handleGetBio: %v", err)
	}
	if resp["summary"] != "Two-sentence summary." {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp["kind"] != "album" {
		t.Fatalf("expected kind=album: %+v", resp)
	}
	if resp["source_url"] != "https://w/ko" {
		t.Fatalf("missing source_url: %+v", resp)
	}
	if svc.getCalled != 1 {
		t.Fatalf("expected 1 service call, got %d", svc.getCalled)
	}
}

func TestBioHandlers_handleGetBio_MissingFields(t *testing.T) {
	t.Parallel()
	svc := &fakeBioService{}
	h := NewBioHandlers(svc)

	if _, err := h.handleGetBioInternal(map[string]interface{}{"artist": "x"}); err == nil {
		t.Fatalf("expected error for missing album")
	}
	if _, err := h.handleGetBioInternal(map[string]interface{}{"album": "y"}); err == nil {
		t.Fatalf("expected error for missing artist")
	}
	if _, err := h.handleGetBioInternal(nil); err == nil {
		t.Fatalf("expected error for nil payload")
	}
}

func TestBioHandlers_handleRefresh_DispatchesToService(t *testing.T) {
	t.Parallel()
	svc := &fakeBioService{out: bios.Bio{Summary: "fresh", Kind: "album"}}
	h := NewBioHandlers(svc)

	resp, err := h.handleRefreshBioInternal(map[string]interface{}{"artist": "X", "album": "Y"})
	if err != nil {
		t.Fatal(err)
	}
	if svc.refreshCalled != 1 || resp["summary"] != "fresh" {
		t.Fatalf("refresh not dispatched: refreshCalled=%d resp=%+v", svc.refreshCalled, resp)
	}
}

func TestBioHandlers_ServiceError_PropagatesEmptyBioGracefully(t *testing.T) {
	t.Parallel()
	svc := &fakeBioService{err: errors.New("upstream fail")}
	h := NewBioHandlers(svc)

	resp, err := h.handleGetBioInternal(map[string]interface{}{"artist": "X", "album": "Y"})
	if err != nil {
		t.Fatalf("handler should swallow service error, returning empty bio: %v", err)
	}
	if resp["summary"] != "" {
		t.Fatalf("expected empty summary on service error: %+v", resp)
	}
	if resp["kind"] != "" {
		t.Fatalf("expected empty kind on service error: %+v", resp)
	}
}

func TestBioHandlers_NilService_NoOp(t *testing.T) {
	t.Parallel()
	h := NewBioHandlers(nil)
	// Should not panic when constructed with nil service; calls are guarded.
	_, err := h.handleGetBioInternal(map[string]interface{}{"artist": "X", "album": "Y"})
	if err == nil {
		t.Fatalf("expected error when service is nil on get")
	}
	_, err = h.handleRefreshBioInternal(map[string]interface{}{"artist": "X", "album": "Y"})
	if err == nil {
		t.Fatalf("expected error when service is nil on refresh")
	}
	if _, err := h.handleRefreshBioInternal(nil); err == nil {
		t.Fatalf("expected error when refresh payload is nil")
	}
}

func TestBioHandlers_handleRefresh_ServiceError_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	svc := &fakeBioService{err: errors.New("upstream"), out: bios.Bio{}}
	h := NewBioHandlers(svc)
	resp, err := h.handleRefreshBioInternal(map[string]interface{}{"artist": "X", "album": "Y"})
	if err != nil {
		t.Fatalf("refresh should swallow service error: %v", err)
	}
	if resp["summary"] != "" {
		t.Fatalf("expected empty summary on refresh service error, got %+v", resp)
	}
}

// blockingBioService blocks GetAlbumBio / RefreshAlbumBio until ctx fires
// or its done channel is closed, whichever comes first. Used to verify the
// handler bounds upstream calls with a context timeout.
type blockingBioService struct {
	ctxErr chan error
}

func (b *blockingBioService) GetAlbumBio(ctx context.Context, _, _ string) (bios.Bio, error) {
	<-ctx.Done()
	if b.ctxErr != nil {
		b.ctxErr <- ctx.Err()
	}
	return bios.Bio{}, ctx.Err()
}

func (b *blockingBioService) RefreshAlbumBio(ctx context.Context, _, _ string) (bios.Bio, error) {
	<-ctx.Done()
	if b.ctxErr != nil {
		b.ctxErr <- ctx.Err()
	}
	return bios.Bio{}, ctx.Err()
}

// TestBioHandlers_GetBio_BoundedByTimeout verifies that a stuck upstream
// (Wikipedia, Anthropic) cannot wedge the handler indefinitely — the
// per-call context.WithTimeout fires and the handler returns within the
// configured bound. Test shrinks bioHandlerTimeout to keep wall time small.
func TestBioHandlers_GetBio_BoundedByTimeout(t *testing.T) {
	prev := bioHandlerTimeout
	bioHandlerTimeout = 50 * time.Millisecond
	t.Cleanup(func() { bioHandlerTimeout = prev })

	svc := &blockingBioService{ctxErr: make(chan error, 1)}
	h := NewBioHandlers(svc)

	done := make(chan struct{})
	go func() {
		// Service swallows ctx errors into empty-bio payload, so err is nil.
		resp, err := h.handleGetBioInternal(map[string]interface{}{"artist": "X", "album": "Y"})
		if err != nil {
			t.Errorf("expected nil err (graceful empty), got %v", err)
		}
		if resp["summary"] != "" {
			t.Errorf("expected empty summary on timeout, got %+v", resp)
		}
		close(done)
	}()

	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("handleGetBioInternal did not return within 2s of a 50ms timeout — context not propagated")
	}

	select {
	case err := <-svc.ctxErr:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected DeadlineExceeded inside service, got %v", err)
		}
	default:
		t.Fatal("service never observed ctx.Done — handler not propagating bounded ctx")
	}
}

// TestBioHandlers_RefreshBio_BoundedByTimeout mirrors the get-path test
// for the rebuild path.
func TestBioHandlers_RefreshBio_BoundedByTimeout(t *testing.T) {
	prev := bioHandlerTimeout
	bioHandlerTimeout = 50 * time.Millisecond
	t.Cleanup(func() { bioHandlerTimeout = prev })

	svc := &blockingBioService{ctxErr: make(chan error, 1)}
	h := NewBioHandlers(svc)

	done := make(chan struct{})
	go func() {
		resp, err := h.handleRefreshBioInternal(map[string]interface{}{"artist": "X", "album": "Y"})
		if err != nil {
			t.Errorf("expected nil err (graceful empty), got %v", err)
		}
		if resp["summary"] != "" {
			t.Errorf("expected empty summary on timeout, got %+v", resp)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleRefreshBioInternal did not return within 2s of a 50ms timeout — context not propagated")
	}

	select {
	case err := <-svc.ctxErr:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected DeadlineExceeded inside service, got %v", err)
		}
	default:
		t.Fatal("service never observed ctx.Done — handler not propagating bounded ctx")
	}
}
