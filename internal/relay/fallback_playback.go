package relay

import (
	"context"
	"errors"
	"github.com/kreuzhofer/restreamer/internal/config"
	"os"
	"path/filepath"
	"time"
)

// The descriptor is verified before the command lock and never owns a running
// decoder. Each entry opens its own cursor; selections do not replace that cursor.
func cloneFallback(source *clipPlayback) (*clipPlayback, error) {
	if source == nil {
		return nil, nil
	}
	reader, err := openClip(source.reader.file.Name(), source.reader.index)
	if err != nil {
		return nil, err
	}
	p := &clipPlayback{ID: source.ID, Revision: source.Revision, Name: source.Name, reader: reader, loop: true}
	if source.presentation != nil {
		v := *source.presentation
		v.runtime = nil
		p.presentation = &v
		reader.onClose = p.presentation.close
	}
	return p, nil
}
func (s StageMediaSelections) fallbackRevision() string {
	if s.FallbackMode == "override" {
		return s.Fallback
	}
	if s.FallbackMode == "shared" {
		return s.BRB
	}
	return ""
}
func (s *Server) fallbackDescriptor(settings StageMediaSelections) (*clipPlayback, error) {
	id := settings.fallbackRevision()
	if id == "" {
		return nil, nil
	}
	p, _, err := s.library.openRevisionLocked(id)
	if err != nil {
		return nil, err
	}
	p.reader.close()
	return p, nil
}
func (s *Server) prepareEmergency() (*brbMedia, error) {
	s.broadcast.mu.Lock()
	if s.broadcast.media == nil {
		s.broadcast.mu.Unlock()
		return nil, errors.New("BRB profile is unavailable")
	}
	profile := s.broadcast.media.settings.Profile
	s.broadcast.mu.Unlock()
	return s.prepareEmergencyProfile(profile)
}
func (s *Server) prepareEmergencyProfile(profile config.BRBProfile) (*brbMedia, error) {
	dir, err := os.MkdirTemp(s.cfg.BRB.Directory, "emergency-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err = defaultBRBImage(filepath.Join(dir, "image.png"), defaultBRBText); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	release, err := s.preparation.acquire(ctx)
	if err != nil {
		return nil, errors.New("Cannot prepare emergency screen while media preparation is busy; retry")
	}
	defer release()
	media, err := encodeBRB(ctx, profile, dir, true, false, 0, defaultBRBText)
	if err != nil {
		return nil, errors.New("Cannot prepare independent silent emergency screen")
	}
	media.settings.Profile = profile
	return media, nil
}
func (b *broadcast) closeFallback() {
	if b.fallback != nil {
		b.fallback.reader.close()
		b.fallback = nil
	}
	b.fallbackMedia = nil
	b.fallbackFailed = false
	b.fallbackError = ""
}
func (b *broadcast) beginFallback(now time.Time) bool {
	if b.fallback != nil {
		return true
	}
	if b.fallbackFailed {
		return false
	}
	if b.fallbackNext == nil {
		if b.fallbackManaged {
			b.fallbackFailure("Selected fallback is unavailable for this profile. Select a ready video in Show setup and recover fallback.", now)
			return false
		}
		if b.fallbackMedia == nil {
			b.fallbackMedia = b.media
		}
		return false
	}
	p, err := cloneFallback(b.fallbackNext)
	if err != nil {
		b.fallbackFailure("Selected fallback video is unavailable. Emergency screen is active; select a ready video and retry.", now)
		return false
	}
	b.control.version++
	b.fallback = p
	b.fallbackMedia = nil
	return true
}
func (b *broadcast) fallbackFailure(reason string, now time.Time) {
	if !b.fallbackFailed {
		b.control.version++
	}
	if b.fallback != nil {
		b.fallback.reader.close()
		b.fallback = nil
	}
	b.fallbackFailed = true
	b.fallbackError = reason
	b.fallbackMedia = b.emergency
	if b.fallbackMedia == nil {
		b.fallbackError = "Fallback and emergency screen are unavailable. Stop or restore prepared media."
		b.active = false
		return
	}
	b.startLegacyFallback(now)
}
func (b *broadcast) tickFallback(now time.Time) {
	if b.fallback == nil {
		if b.fallbackMedia != nil {
			b.sendFallback(now)
		}
		return
	}
	// Reuse the bounded playback engine with its own cursor. The operator's
	// intended/paused source and failure state remain separate from fallback.
	primary, failed, manual, playbackError := b.clip, b.control.failed, b.manual, b.playbackError
	b.clip, b.control.failed, b.manual = b.fallback, false, false
	b.playingFallback = true
	b.tickClip(now)
	b.playingFallback = false
	b.clip, b.control.failed, b.manual, b.playbackError = primary, failed, manual, playbackError
	b.live = false
	b.active = b.fallback != nil || b.fallbackMedia != nil
}
