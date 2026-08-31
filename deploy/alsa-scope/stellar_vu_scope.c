/*
 * stellar_vu_scope — an ALSA "scope" plugin that tees the DAC stream to a FIFO
 * for the LCD VU meter.
 *
 * WHY THIS EXISTS
 * ---------------
 * The VU meter used to be fed by a second MPD `audio_output` (a fifo output).
 * That does not work, and cannot be made to work by tuning:
 *
 *   MPD's MultipleOutputs returns a decoded chunk to the shared MusicBuffer
 *   pool only once EVERY enabled output has consumed it. A second output is a
 *   second *consumer*, so it sits on the DAC's critical path by construction.
 *   Anything that slows it — a resampler, a bit-depth conversion, a scheduling
 *   hiccup in the reader — starves the decoder feeding the DAC and underruns
 *   the ALSA ring buffer. MPD misreports this as "Decoder is too slow".
 *
 * Measured on the Pi 2026-08-26/31: pinning the FIFO output to 44100:16:2 put a
 * resampler in its chain and produced ~1 XRUN/min at 192 kHz. Removing the
 * resampler (format "*:16:2") cut that ~10x but not to zero — ~1 XRUN per
 * 5-10 min remained, from the bit-depth conversion and the coupling itself.
 * With the FIFO output disabled: 0 XRUN in 19 minutes.
 *
 * This plugin removes the second consumer entirely. ALSA's `type meter` PCM is
 * a pass-through that hands each buffer to scope callbacks while forwarding the
 * identical samples to the slave device. There is one consumer (the DAC), no
 * shared chunk pool and nothing to retain.
 *
 * BIT-PERFECT
 * -----------
 * The meter plugin does not convert. Verified on the Singxer SU-6: DSD512
 * arrives as DSD_U32_BE @ 705600 and 192 kHz/24 FLAC as S32_LE @ 192000,
 * byte-identical to raw hw:2,0. `aplay --dump-hw-params` through the meter
 * advertises the card's own format list and *rejects* formats the card cannot
 * do — a plug/convert layer would have silently accepted them.
 *
 * NEVER BLOCK
 * -----------
 * update() runs on the audio thread. The FIFO is opened O_NONBLOCK and a short
 * write is DROPPED, never retried. A stalled or absent reader must cost the
 * audio path nothing — that is the whole point of this file. If the backend is
 * not reading, we lose meter frames and the DAC does not care.
 *
 * OUTPUT FORMAT
 * -------------
 * Interleaved signed 16-bit little-endian, native channel count and rate, which
 * is what internal/infra/spectrum already parses. It recovers the sample rate
 * from how fast windows arrive, so nothing needs to be told about rate changes.
 *
 * Config (in /etc/asound.conf):
 *
 *   pcm.stellar_dac {
 *       type meter
 *       slave.pcm "hw:2,0"
 *       scopes.0 {
 *           type stellar_vu
 *           fifo "/tmp/mpd_spectrum.fifo"
 *       }
 *   }
 */

#include <alsa/asoundlib.h>

#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#define DEFAULT_FIFO "/tmp/mpd_spectrum.fifo"

/* How many frames we are willing to stage per update() call. Anything beyond
 * this in a single callback is dropped rather than looped over — the audio
 * thread must not spend unbounded time here. 8192 frames is 43 ms at 192 kHz,
 * far more than a normal period. */
#define MAX_STAGE_FRAMES 8192

/* Reopen attempts are rate-limited to one per this many update() calls, so a
 * missing reader costs an open(2) rarely rather than on every period. */
#define REOPEN_EVERY 64

typedef struct {
	snd_pcm_t *pcm;
	snd_pcm_scope_t *s16; /* alsa-lib's S16 conversion scope; we read its buffers */
	char *fifo_path;
	int fd; /* -1 when not open */
	unsigned int reopen_countdown;

	snd_pcm_uframes_t last; /* meter position we have already forwarded */
	unsigned int channels;
	snd_pcm_uframes_t bufsize;
	snd_pcm_uframes_t boundary;

	int16_t *stage; /* interleave staging buffer, MAX_STAGE_FRAMES * channels */
} stellar_vu_t;

/* Which slave formats alsa-lib's own s16 conversion scope can actually handle.
 *
 * This list is copied verbatim from s16_enable() in alsa-lib's src/pcm/pcm_meter.c
 * and must stay in sync with it. It matters because of a trap in the scope API:
 * snd_pcm_scope_enable() records `scope->enabled = (err >= 0)` and then DISCARDS
 * the error. So when s16_enable() hits its `default: return -EINVAL` branch — DSD,
 * S24_3LE, float — the s16 scope is quietly left disabled with buf_areas == NULL,
 * while *our* scope is still enabled and still gets update() calls. The next
 * snd_pcm_scope_s16_get_channel_buffer() then trips `assert(s16->buf_areas)` and
 * aborts the whole process. That is not theoretical: it killed mpd mid-track the
 * first time a DSD512 file was played through this plugin.
 *
 * There is no public way to ask whether another scope enabled, so we re-derive
 * the answer from the format and decline to enable when s16 would have declined. */
static int stellar_vu_format_meterable(snd_pcm_format_t format)
{
	switch (format) {
	case SND_PCM_FORMAT_A_LAW:
	case SND_PCM_FORMAT_MU_LAW:
	case SND_PCM_FORMAT_IMA_ADPCM:
	case SND_PCM_FORMAT_S8:
	case SND_PCM_FORMAT_S16_LE:
	case SND_PCM_FORMAT_S16_BE:
	case SND_PCM_FORMAT_S24_LE:
	case SND_PCM_FORMAT_S24_BE:
	case SND_PCM_FORMAT_S32_LE:
	case SND_PCM_FORMAT_S32_BE:
	case SND_PCM_FORMAT_U8:
	case SND_PCM_FORMAT_U16_LE:
	case SND_PCM_FORMAT_U16_BE:
	case SND_PCM_FORMAT_U24_LE:
	case SND_PCM_FORMAT_U24_BE:
	case SND_PCM_FORMAT_U32_LE:
	case SND_PCM_FORMAT_U32_BE:
		return 1;
	default:
		/* DSD_U8/U16/U32, S24_3LE/BE, FLOAT, MPEG, GSM, ... */
		return 0;
	}
}

/* ------------------------------------------------------------------ FIFO --*/

/* Open the FIFO for writing without blocking. A FIFO with no reader returns
 * ENXIO for O_WRONLY|O_NONBLOCK, which is the normal case when the backend is
 * down — treat it as "not now", not as an error. */
static void vu_try_open(stellar_vu_t *vu)
{
	if (vu->fd >= 0)
		return;
	if (vu->reopen_countdown > 0) {
		vu->reopen_countdown--;
		return;
	}
	vu->reopen_countdown = REOPEN_EVERY;

	int fd = open(vu->fifo_path, O_WRONLY | O_NONBLOCK | O_CLOEXEC);
	if (fd < 0)
		return;
	vu->fd = fd;
}

/* Write once, non-blocking. Drop on EAGAIN or a short write: the meter is
 * disposable, the audio stream is not. Close on a real error so the next
 * update() retries the open. */
static void vu_write(stellar_vu_t *vu, const void *buf, size_t bytes)
{
	if (vu->fd < 0 || bytes == 0)
		return;

	ssize_t n = write(vu->fd, buf, bytes);
	if (n >= 0)
		return; /* short writes are dropped deliberately */

	if (errno == EAGAIN || errno == EWOULDBLOCK || errno == EINTR)
		return; /* reader is behind — drop this window */

	/* EPIPE (reader went away) or anything else: reopen later. */
	close(vu->fd);
	vu->fd = -1;
}

/* ---------------------------------------------------------------- scope ---*/

static int stellar_vu_enable(snd_pcm_scope_t *scope)
{
	stellar_vu_t *vu = snd_pcm_scope_get_callback_private(scope);
	snd_pcm_hw_params_t *hw;
	snd_pcm_format_t format;
	snd_pcm_access_t access;

	/* Decline for any stream the s16 scope cannot convert — see the comment on
	 * stellar_vu_format_meterable(). Returning < 0 here makes the meter mark us
	 * disabled and skip update() entirely, which is exactly what we want: DSD
	 * plays bit-perfect and the VU meter simply has nothing to show. enable() is
	 * re-run per hw_params, so the next PCM track re-arms the tap by itself. */
	snd_pcm_hw_params_alloca(&hw);
	if (snd_pcm_hw_params_current(vu->pcm, hw) < 0)
		return -EINVAL;
	if (snd_pcm_hw_params_get_format(hw, &format) < 0)
		return -EINVAL;
	if (!stellar_vu_format_meterable(format))
		return -EINVAL;
	/* s16_enable() has one more refusal: S16 already in MMAP_NONINTERLEAVED is
	 * its would-be zero-copy path, and it bails out of that with -EINVAL too. */
	if ((format == SND_PCM_FORMAT_S16_LE || format == SND_PCM_FORMAT_S16_BE) &&
	    snd_pcm_hw_params_get_access(hw, &access) >= 0 &&
	    access == SND_PCM_ACCESS_MMAP_NONINTERLEAVED)
		return -EINVAL;

	vu->channels = snd_pcm_meter_get_channels(vu->pcm);
	vu->bufsize = snd_pcm_meter_get_bufsize(vu->pcm);
	vu->boundary = snd_pcm_meter_get_boundary(vu->pcm);
	vu->last = snd_pcm_meter_get_now(vu->pcm);

	if (vu->channels == 0)
		return -EINVAL;

	free(vu->stage);
	vu->stage = malloc(sizeof(int16_t) * MAX_STAGE_FRAMES * vu->channels);
	if (vu->stage == NULL)
		return -ENOMEM;

	return 0;
}

static void stellar_vu_disable(snd_pcm_scope_t *scope)
{
	stellar_vu_t *vu = snd_pcm_scope_get_callback_private(scope);

	free(vu->stage);
	vu->stage = NULL;
	if (vu->fd >= 0) {
		close(vu->fd);
		vu->fd = -1;
	}
}

static void stellar_vu_start(snd_pcm_scope_t *scope)
{
	stellar_vu_t *vu = snd_pcm_scope_get_callback_private(scope);
	vu->last = snd_pcm_meter_get_now(vu->pcm);
	vu->reopen_countdown = 0; /* try to attach immediately on start */
}

static void stellar_vu_stop(snd_pcm_scope_t *scope)
{
	stellar_vu_t *vu = snd_pcm_scope_get_callback_private(scope);
	if (vu->fd >= 0) {
		close(vu->fd);
		vu->fd = -1;
	}
}

static void stellar_vu_reset(snd_pcm_scope_t *scope)
{
	stellar_vu_t *vu = snd_pcm_scope_get_callback_private(scope);
	vu->last = snd_pcm_meter_get_now(vu->pcm);
}

static void stellar_vu_update(snd_pcm_scope_t *scope)
{
	stellar_vu_t *vu = snd_pcm_scope_get_callback_private(scope);

	snd_pcm_uframes_t now = snd_pcm_meter_get_now(vu->pcm);
	snd_pcm_sframes_t avail = (snd_pcm_sframes_t)(now - vu->last);
	if (avail < 0)
		avail += vu->boundary; /* meter position wrapped */
	if (avail <= 0)
		return;

	/* If we have fallen further behind than the ring holds, the old samples
	 * are already overwritten. Skip forward rather than emit garbage. */
	if ((snd_pcm_uframes_t)avail > vu->bufsize) {
		vu->last = now - vu->bufsize;
		avail = (snd_pcm_sframes_t)vu->bufsize;
	}
	if ((snd_pcm_uframes_t)avail > MAX_STAGE_FRAMES) {
		/* Drop the excess; the meter does not need every frame. */
		vu->last = now - MAX_STAGE_FRAMES;
		avail = MAX_STAGE_FRAMES;
	}

	vu_try_open(vu);
	if (vu->fd < 0) {
		vu->last = now; /* stay in sync even while nobody is listening */
		return;
	}

	unsigned int channels = vu->channels;
	snd_pcm_uframes_t offset = vu->last % vu->bufsize;
	int16_t *stage = vu->stage;

	for (unsigned int c = 0; c < channels; c++) {
		int16_t *src = snd_pcm_scope_s16_get_channel_buffer(vu->s16, c);
		if (src == NULL) {
			vu->last = now;
			return;
		}
		for (snd_pcm_sframes_t i = 0; i < avail; i++) {
			snd_pcm_uframes_t idx = (offset + (snd_pcm_uframes_t)i) % vu->bufsize;
			stage[(size_t)i * channels + c] = src[idx];
		}
	}

	vu_write(vu, stage, (size_t)avail * channels * sizeof(int16_t));
	vu->last = now;
}

static void stellar_vu_close(snd_pcm_scope_t *scope)
{
	stellar_vu_t *vu = snd_pcm_scope_get_callback_private(scope);

	if (vu->fd >= 0)
		close(vu->fd);
	free(vu->stage);
	free(vu->fifo_path);
	free(vu);
}

static const snd_pcm_scope_ops_t stellar_vu_ops = {
	.enable = stellar_vu_enable,
	.disable = stellar_vu_disable,
	.start = stellar_vu_start,
	.stop = stellar_vu_stop,
	.update = stellar_vu_update,
	.reset = stellar_vu_reset,
	.close = stellar_vu_close,
};

/* ----------------------------------------------------------------- open ---*/

int _snd_pcm_scope_stellar_vu_open(snd_pcm_t *pcm, const char *name,
				   snd_config_t *root, snd_config_t *conf)
{
	snd_config_iterator_t i, next;
	const char *fifo = DEFAULT_FIFO;
	int err;

	(void)root;

	snd_config_for_each(i, next, conf) {
		snd_config_t *n = snd_config_iterator_entry(i);
		const char *id;

		if (snd_config_get_id(n, &id) < 0)
			continue;
		if (strcmp(id, "comment") == 0 || strcmp(id, "type") == 0)
			continue;
		if (strcmp(id, "fifo") == 0) {
			if (snd_config_get_string(n, &fifo) < 0) {
				SNDERR("stellar_vu: `fifo` must be a string");
				return -EINVAL;
			}
			continue;
		}
		SNDERR("stellar_vu: unknown field %s", id);
		return -EINVAL;
	}

	stellar_vu_t *vu = calloc(1, sizeof(*vu));
	if (vu == NULL)
		return -ENOMEM;

	vu->pcm = pcm;
	vu->fd = -1;
	vu->fifo_path = strdup(fifo);
	if (vu->fifo_path == NULL) {
		free(vu);
		return -ENOMEM;
	}

	/* Make sure the FIFO exists so the reader can attach whenever it likes.
	 * An existing node (of any kind) is left alone. */
	if (mkfifo(vu->fifo_path, 0666) < 0 && errno != EEXIST) {
		SNDERR("stellar_vu: cannot create FIFO %s: %s", vu->fifo_path,
		       strerror(errno));
		free(vu->fifo_path);
		free(vu);
		return -errno;
	}

	/* alsa-lib's S16 scope does the format conversion for us and exposes
	 * per-channel int16 ring buffers. Reuse one if the config already
	 * declared it, otherwise create it. */
	vu->s16 = snd_pcm_meter_search_scope(pcm, "s16");
	if (vu->s16 == NULL) {
		err = snd_pcm_scope_s16_open(pcm, "s16", &vu->s16);
		if (err < 0) {
			SNDERR("stellar_vu: cannot open the s16 scope: %s",
			       snd_strerror(err));
			free(vu->fifo_path);
			free(vu);
			return err;
		}
	}

	snd_pcm_scope_t *scope;
	err = snd_pcm_scope_malloc(&scope);
	if (err < 0) {
		free(vu->fifo_path);
		free(vu);
		return err;
	}

	snd_pcm_scope_set_ops(scope, &stellar_vu_ops);
	snd_pcm_scope_set_name(scope, name);
	snd_pcm_scope_set_callback_private(scope, vu);

	err = snd_pcm_meter_add_scope(pcm, scope);
	if (err < 0) {
		free(vu->fifo_path);
		free(vu);
		return err;
	}

	return 0;
}

SND_DLSYM_BUILD_VERSION(_snd_pcm_scope_stellar_vu_open, SND_PCM_DLSYM_VERSION);
