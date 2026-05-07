package lastplayed_test

import (
	"errors"
	"testing"
	"time"

	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/domain/lastplayed"
	"github.com/edumarques81/stellar-volumio-audioplayer-backend/internal/infra/cache"
)

type fakeStore struct {
	puts   []cache.LastPlayedAlbum
	getRow cache.LastPlayedAlbum
	getOK  bool
	getErr error
	putErr error
}

func (f *fakeStore) Put(row cache.LastPlayedAlbum) error {
	if f.putErr != nil {
		return f.putErr
	}
	f.puts = append(f.puts, row)
	return nil
}

func (f *fakeStore) GetMostRecent() (cache.LastPlayedAlbum, bool, error) {
	return f.getRow, f.getOK, f.getErr
}

func TestService_Record_StampsTimestampAndPersists(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	svc := lastplayed.NewService(store)

	before := time.Now().Unix()
	err := svc.Record(lastplayed.Album{
		Artist:   "Miles Davis",
		Album:    "Kind of Blue",
		AlbumArt: "/albumart?path=mile/kob/01.flac",
		TrackURI: "NAS/Miles Davis/Kind of Blue/01.flac",
	})
	after := time.Now().Unix()

	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(store.puts) != 1 {
		t.Fatalf("expected 1 put, got %d", len(store.puts))
	}
	row := store.puts[0]
	if row.LastPlayedAt < before || row.LastPlayedAt > after {
		t.Fatalf("LastPlayedAt not stamped to ~now: %d (window %d..%d)", row.LastPlayedAt, before, after)
	}
	if row.Artist != "Miles Davis" || row.Album != "Kind of Blue" {
		t.Fatalf("unexpected row contents: %+v", row)
	}
}

func TestService_Record_NoOpOnMissingArtistOrAlbum(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	svc := lastplayed.NewService(store)

	if err := svc.Record(lastplayed.Album{Album: "x"}); err != nil {
		t.Fatalf("expected nil err on missing artist, got %v", err)
	}
	if err := svc.Record(lastplayed.Album{Artist: "x"}); err != nil {
		t.Fatalf("expected nil err on missing album, got %v", err)
	}
	if err := svc.Record(lastplayed.Album{Artist: "  ", Album: "  "}); err != nil {
		t.Fatalf("expected nil err on whitespace-only fields, got %v", err)
	}
	if len(store.puts) != 0 {
		t.Fatalf("expected zero puts on incomplete metadata, got %d", len(store.puts))
	}
}

func TestService_Record_PropagatesStoreError(t *testing.T) {
	t.Parallel()
	boom := errors.New("disk full")
	store := &fakeStore{putErr: boom}
	svc := lastplayed.NewService(store)
	err := svc.Record(lastplayed.Album{Artist: "x", Album: "y"})
	if !errors.Is(err, boom) {
		t.Fatalf("expected store error to propagate, got %v", err)
	}
}

func TestService_Record_ReturnsErrNoStoreWhenStoreNil(t *testing.T) {
	t.Parallel()
	svc := lastplayed.NewService(nil)
	if err := svc.Record(lastplayed.Album{Artist: "x", Album: "y"}); !errors.Is(err, lastplayed.ErrNoStore) {
		t.Fatalf("expected ErrNoStore, got %v", err)
	}
}

func TestService_Get_Hit(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		getRow: cache.LastPlayedAlbum{
			Artist: "Coltrane", Album: "A Love Supreme", AlbumArt: "/art",
			TrackURI: "uri", TrackType: "flac", SampleRate: "96000", BitDepth: "24",
			LastPlayedAt: 12345,
		},
		getOK: true,
	}
	svc := lastplayed.NewService(store)

	got, ok, err := svc.Get()
	if err != nil || !ok {
		t.Fatalf("expected hit, got ok=%v err=%v", ok, err)
	}
	if got.Artist != "Coltrane" || got.Album != "A Love Supreme" || got.TrackType != "flac" {
		t.Fatalf("unexpected album: %+v", got)
	}
}

func TestService_Get_Miss(t *testing.T) {
	t.Parallel()
	svc := lastplayed.NewService(&fakeStore{})
	got, ok, err := svc.Get()
	if err != nil || ok {
		t.Fatalf("expected miss, got ok=%v err=%v album=%+v", ok, err, got)
	}
}

func TestService_Get_ErrNoStoreWhenStoreNil(t *testing.T) {
	t.Parallel()
	svc := lastplayed.NewService(nil)
	if _, _, err := svc.Get(); !errors.Is(err, lastplayed.ErrNoStore) {
		t.Fatalf("expected ErrNoStore, got %v", err)
	}
}
