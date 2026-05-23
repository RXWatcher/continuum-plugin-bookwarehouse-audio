// Package covers serves audiobook cover images from the local filesystem,
// extracting embedded artwork from audio files when no sidecar cover is
// present. Extracted and resized variants are cached to disk keyed by the
// source file's mtime+size so subsequent reads avoid recomputation.
package covers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dhowden/tag"
	"golang.org/x/image/draw"

	"github.com/RXWatcher/silo-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/RXWatcher/silo-plugin-bookwarehouse-audio/internal/localfs"
)

// Size is the requested cover variant.
type Size string

const (
	SizeThumb    Size = "thumb"
	SizeMedium   Size = "medium"
	SizeOriginal Size = "original"
)

// pixelTargets are the longest-edge pixel sizes for thumb/medium variants.
var pixelTargets = map[Size]int{
	SizeThumb:  250,
	SizeMedium: 500,
}

// maxCoverPixels bounds the decoded image dimensions. Mirrors local-audiobooks's
// guard; defends against decompression-bomb DoS on attacker-controlled covers.
const maxCoverPixels = 40_000_000

// sidecarFilenames is the priority order for cover discovery in a book's
// directory. The list is intentionally short — covers shipping under other
// names fall through to embedded extraction.
var sidecarFilenames = []string{
	"cover.jpg", "cover.jpeg", "cover.png",
	"folder.jpg", "folder.jpeg", "folder.png",
}

// ErrNoCover indicates the book exists but no cover bytes were resolvable
// (no sidecar file and no embedded picture).
var ErrNoCover = errors.New("no cover available")

// catalogClient is the subset of bookwarehouse.Client needed to fetch a
// book's file list. Defined as an interface so tests can fake it.
type catalogClient interface {
	GetBook(ctx context.Context, id string) (bookwarehouse.BookDetail, error)
}

// Service resolves cover bytes via the local filesystem and a disk cache.
type Service struct {
	client   catalogClient
	resolver *localfs.Resolver
	cacheDir string

	mu     sync.Mutex
	inProg map[string]chan struct{} // single-flight per cache key
}

// NewService builds a Service. cacheDir must be writable; it is created if
// missing.
func NewService(client catalogClient, resolver *localfs.Resolver, cacheDir string) (*Service, error) {
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "silo-bw-audio-covers")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	return &Service{
		client:   client,
		resolver: resolver,
		cacheDir: cacheDir,
		inProg:   make(map[string]chan struct{}),
	}, nil
}

// Result describes resolved cover bytes ready for http.ServeContent.
type Result struct {
	Reader      io.ReadSeeker
	ContentType string
	ModTime     time.Time
	closer      io.Closer
}

// Close releases any underlying file handle.
func (r *Result) Close() error {
	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

// Get resolves the cover for bookID at the requested size.
func (s *Service) Get(ctx context.Context, bookID string, size Size) (*Result, error) {
	if s.client == nil {
		return nil, fmt.Errorf("covers: catalog client not configured")
	}
	if s.resolver == nil || !s.resolver.Configured() {
		return nil, fmt.Errorf("covers: library_root not configured")
	}
	detail, err := s.client.GetBook(ctx, bookID)
	if err != nil {
		return nil, fmt.Errorf("fetch book detail: %w", err)
	}
	if len(detail.Files) == 0 {
		return nil, ErrNoCover
	}

	firstAudio, audioErr := s.resolver.Resolve(detail.Files[0].StorageKey)
	if audioErr != nil && detail.Files[0].Filename != "" {
		firstAudio, audioErr = s.resolver.Resolve(detail.Files[0].Filename)
	}
	if audioErr != nil {
		return nil, audioErr
	}

	bookDir := filepath.Dir(firstAudio)

	if path, ok := s.findSidecar(bookDir); ok {
		return s.serveSidecar(bookID, path, size)
	}
	return s.serveEmbedded(bookID, firstAudio, size)
}

func (s *Service) findSidecar(dir string) (string, bool) {
	for _, name := range sidecarFilenames {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func (s *Service) serveSidecar(bookID, sourcePath string, size Size) (*Result, error) {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("stat sidecar: %w", err)
	}
	if size == SizeOriginal || pixelTargets[size] == 0 {
		f, err := os.Open(sourcePath)
		if err != nil {
			return nil, fmt.Errorf("open sidecar: %w", err)
		}
		return &Result{
			Reader:      f,
			ContentType: detectImageType(sourcePath),
			ModTime:     info.ModTime(),
			closer:      f,
		}, nil
	}
	return s.resizedFrom(bookID, sourcePath, info, size, "sidecar")
}

func (s *Service) serveEmbedded(bookID, audioPath string, size Size) (*Result, error) {
	info, err := os.Stat(audioPath)
	if err != nil {
		return nil, fmt.Errorf("stat audio: %w", err)
	}
	originalKey := cacheKey(bookID, audioPath, info, SizeOriginal, "embedded")
	originalPath := s.cachePath(originalKey)

	if _, err := os.Stat(originalPath); err != nil {
		if err := s.extractEmbedded(bookID, audioPath, info, originalPath); err != nil {
			return nil, err
		}
	}

	if size == SizeOriginal || pixelTargets[size] == 0 {
		return openResult(originalPath)
	}
	// Use original cache file's info for the resized key so the resized cache
	// invalidates when the audio's embedded art is re-extracted.
	originalInfo, err := os.Stat(originalPath)
	if err != nil {
		return nil, fmt.Errorf("stat extracted cover: %w", err)
	}
	return s.resizedFrom(bookID, originalPath, originalInfo, size, "embedded-resized")
}

func (s *Service) resizedFrom(bookID, sourcePath string, info os.FileInfo, size Size, kind string) (*Result, error) {
	key := cacheKey(bookID, sourcePath, info, size, kind)
	cachePath := s.cachePath(key)
	if r, ok := tryOpen(cachePath); ok {
		return r, nil
	}

	release := s.acquire(key)
	defer release()

	if r, ok := tryOpen(cachePath); ok {
		return r, nil
	}

	src, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("read source: %w", err)
	}
	resized, err := resizeJPEG(src, pixelTargets[size])
	if err != nil {
		return nil, err
	}
	if err := writeCacheFile(cachePath, resized); err != nil {
		return nil, err
	}
	return openResult(cachePath)
}

func (s *Service) extractEmbedded(bookID, audioPath string, audioInfo os.FileInfo, destPath string) error {
	key := cacheKey(bookID, audioPath, audioInfo, SizeOriginal, "embedded")
	release := s.acquire(key)
	defer release()

	if _, err := os.Stat(destPath); err == nil {
		return nil
	}

	f, err := os.Open(audioPath)
	if err != nil {
		return fmt.Errorf("open audio: %w", err)
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return fmt.Errorf("read tag: %w", err)
	}
	picture := m.Picture()
	if picture == nil || len(picture.Data) == 0 {
		return ErrNoCover
	}
	return writeCacheFile(destPath, picture.Data)
}

func (s *Service) cachePath(key string) string {
	return filepath.Join(s.cacheDir, key[:2], key+".bin")
}

// acquire returns a release function for single-flighting a cache key.
func (s *Service) acquire(key string) func() {
	s.mu.Lock()
	wait, busy := s.inProg[key]
	if busy {
		s.mu.Unlock()
		<-wait
		return func() {}
	}
	ch := make(chan struct{})
	s.inProg[key] = ch
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.inProg, key)
		s.mu.Unlock()
		close(ch)
	}
}

func cacheKey(bookID, sourcePath string, info os.FileInfo, size Size, kind string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%d\x00%d\x00%s\x00%s", bookID, sourcePath, info.Size(), info.ModTime().UnixNano(), size, kind)
	return hex.EncodeToString(h.Sum(nil))
}

// tryOpen returns a *Result for path if it exists and is readable.
func tryOpen(path string) (*Result, bool) {
	r, err := openResult(path)
	if err != nil {
		return nil, false
	}
	return r, true
}

// openResult opens path and returns a *Result with content-type sniffed from
// the leading bytes. Caller must Close the result.
func openResult(path string) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	head := make([]byte, 512)
	n, _ := f.Read(head)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Result{
		Reader:      f,
		ContentType: http.DetectContentType(head[:n]),
		ModTime:     info.ModTime(),
		closer:      f,
	}, nil
}

func writeCacheFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache subdir: %w", err)
	}
	tmp := path + ".part"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write cache temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit cache: %w", err)
	}
	return nil
}

func detectImageType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

func resizeJPEG(in []byte, target int) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(in))
	if err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxCoverPixels {
		return nil, fmt.Errorf("cover dimensions exceed limit (%dx%d)", cfg.Width, cfg.Height)
	}
	src, _, err := image.Decode(bytes.NewReader(in))
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	b := src.Bounds()
	longEdge := b.Dx()
	if b.Dy() > longEdge {
		longEdge = b.Dy()
	}
	if longEdge <= target {
		return in, nil
	}
	ratio := float64(target) / float64(longEdge)
	dstW := int(float64(b.Dx()) * ratio)
	dstH := int(float64(b.Dy()) * ratio)
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}
