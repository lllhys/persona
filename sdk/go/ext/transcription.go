package ext

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/nwverse/persona/sdk/go/wire"
)

// Transcription for extensions (docs/extensions/extensions.md "Transcription for extensions", #8485): with the
// permission transcription, the extension's own audio (PCM16 little-endian, mono, 16 kHz) goes to the transcription
// the owner chose for meetings. One job per extension at a time; errors carry wire.TranscriptionErrorCode* or
// permission_denied. Transcribe does the whole of it.

// TranscriptionChunkMost is the most PCM one TranscriptionAudio carries (8 seconds).
const TranscriptionChunkMost = 256 << 10

// TranscriptionStart opens a job.
func (h *Hub) TranscriptionStart(ctx context.Context, p wire.TranscriptionStartParams) (wire.TranscriptionStartResult, error) {
	return callHub[wire.TranscriptionStartResult](ctx, h, "transcription.start", p)
}

// TranscriptionAudio sends the next piece of a job's audio (at most TranscriptionChunkMost bytes of PCM, contiguous
// with the last); it returns once the transcription has room for more, which paces the job.
func (h *Hub) TranscriptionAudio(ctx context.Context, job string, pcm []byte) error {
	return h.Call(ctx, "transcription.audio", wire.TranscriptionAudioParams{Job: job, Data: base64.StdEncoding.EncodeToString(pcm)}, nil)
}

// TranscriptionFinish says every piece is sent and waits for the text.
func (h *Hub) TranscriptionFinish(ctx context.Context, job string) (wire.TranscriptionFinishResult, error) {
	return callHub[wire.TranscriptionFinishResult](ctx, h, "transcription.finish", wire.TranscriptionJobParams{Job: job})
}

// TranscriptionCancel drops a job and its text.
func (h *Hub) TranscriptionCancel(ctx context.Context, job string) error {
	return h.Call(ctx, "transcription.cancel", wire.TranscriptionJobParams{Job: job}, nil)
}

// Transcribe turns pcm (PCM16 little-endian, mono, 16 kHz; at most 15 minutes) into sentences: a job, its audio in
// pieces, and its finish; a job that fails on the way is cancelled. Gaps in the result are spans the transcription did
// not trust any text for: say so, never guess.
func Transcribe(ctx context.Context, h *Hub, pcm []byte, p wire.TranscriptionStartParams) (wire.TranscriptionFinishResult, error) {
	if len(pcm)%2 != 0 {
		return wire.TranscriptionFinishResult{}, errors.New("transcribe: PCM16 has an even number of bytes")
	}
	start, err := h.TranscriptionStart(ctx, p)
	if err != nil {
		return wire.TranscriptionFinishResult{}, err
	}
	for len(pcm) > 0 {
		n := min(len(pcm), TranscriptionChunkMost)
		if err := h.TranscriptionAudio(ctx, start.Job, pcm[:n]); err != nil {
			_ = h.TranscriptionCancel(context.WithoutCancel(ctx), start.Job)
			return wire.TranscriptionFinishResult{}, err
		}
		pcm = pcm[n:]
	}
	out, err := h.TranscriptionFinish(ctx, start.Job)
	if err != nil {
		_ = h.TranscriptionCancel(context.WithoutCancel(ctx), start.Job)
	}
	return out, err
}
