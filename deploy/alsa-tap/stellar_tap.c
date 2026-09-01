/*
 * stellar_tap — a transparent ALSA pass-through that copies the DAC stream to a
 * FIFO for the LCD VU meter.
 *
 * WHY THIS EXISTS
 * ---------------
 * The VU meter must never be a second MPD output. MPD returns a decoded chunk to
 * its shared MusicBuffer pool only once EVERY enabled output has consumed it, so
 * a second output sits on the DAC's critical path by construction and starves the
 * decoder. That cost ~1 XRUN per 5-10 min until 2026-08-31.
 *
 * The first fix used ALSA's built-in `type meter` PCM with a custom scope plugin.
 * That worked for PCM but could not meter DSD: the only public way for a scope to
 * read samples is snd_pcm_scope_s16_get_channel_buffer(), and alsa-lib's s16
 * converter returns -EINVAL for DSD_U32_BE. Worse, snd_pcm_scope_enable() discards
 * that error, so the s16 scope ends up disabled with buf_areas == NULL while our
 * scope keeps receiving update() — and the next accessor call trips
 * `assert(s16->buf_areas)` and aborts mpd mid-track.
 *
 * Vendoring alsa-lib's meter to get at its raw buffer is not possible either: 44 of
 * the 76 snd_pcm_* symbols pcm_meter.c needs (snd_pcm_new, snd_pcm_open_slave,
 * snd_pcm_hw_params_slave, _snd_pcm_hw_params_internal, snd_pcm_link_hw_ptr,
 * snd_pcm_mmap_areas, snd_pcm_linear_convert*, ...) are not exported by libasound.
 *
 * snd_pcm_extplug is the way through. It is public, exported and stable, alsa-lib
 * owns the slave, and transfer() hands us the raw areas for EVERY format the card
 * accepts — DSD included. Verified: the card's own list survives the plugin
 * (S16_LE S32_LE SPECIAL DSD_U32_BE, RATE [44100 768000]) and playback negotiates
 * format == slave_format at 44.1/16, 192/24 and DSD512.
 *
 * THREADING
 * ---------
 * transfer() runs on the caller's audio thread, so it does the cheapest possible
 * thing: one snd_pcm_areas_copy() pass-through, then a bounded conversion into a
 * lock-free SPSC ring. A separate writer thread drains that ring into the FIFO.
 * Nothing on the audio thread can block: the ring drops on overflow and the FIFO
 * write is O_NONBLOCK with drop-on-full. This mirrors what `type meter` did (it
 * also copied on the caller's thread and ran scopes on its own thread).
 */

#define _GNU_SOURCE
#include <alsa/asoundlib.h>
#include <alsa/pcm_external.h>
#include <alsa/pcm_extplug.h>

#include <errno.h>
#include <fcntl.h>
#include <pthread.h>
#include <signal.h>
#include <stdatomic.h>
#include <stdint.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#define DEFAULT_FIFO   "/tmp/mpd_spectrum.fifo"

/* Ring holds interleaved stereo S16. 64 KiB is ~370 ms at 44.1 kHz stereo and
 * ~85 ms at 192 kHz — far more than the writer thread's 5 ms poll needs. */
#define RING_BYTES     (64u * 1024u)

/* The backend infers the stream rate from how fast windows arrive, so PCM is
 * forwarded at its native rate. DSD is decimated to land near this. */
#define DSD_TARGET_RATE 44100u

typedef enum {
	TAP_MODE_NONE = 0,  /* format we cannot meter — pass through silently */
	TAP_MODE_S16,
	TAP_MODE_S32,       /* also covers 24-in-32 */
	TAP_MODE_DSD_U32
} tap_mode_t;

typedef struct {
	snd_pcm_extplug_t ext;

	char *fifo_path;
	tap_mode_t mode;
	unsigned int channels;

	/* DSD decimation state */
	unsigned int dsd_decim;    /* input frames per output sample */
	unsigned int dsd_count[2]; /* popcount accumulator, per channel */
	unsigned int dsd_frames;   /* frames accumulated so far */

	/* lock-free SPSC ring: producer = audio thread, consumer = writer thread */
	unsigned char *ring;
	_Atomic unsigned int head; /* producer writes */
	_Atomic unsigned int tail; /* consumer writes */

	/* Exact output byte rate (stereo S16), published for the writer to pace to.
	 * 0 until the first hw_params. */
	_Atomic unsigned int out_byterate;

	pthread_t writer;
	_Atomic int writer_running;
	int writer_started;
} stellar_tap_t;

/* ------------------------------------------------------------------- ring --*/

static void ring_put(stellar_tap_t *tap, const void *buf, unsigned int bytes)
{
	unsigned int head = atomic_load_explicit(&tap->head, memory_order_relaxed);
	unsigned int tail = atomic_load_explicit(&tap->tail, memory_order_acquire);
	unsigned int used = head - tail;
	unsigned int free_bytes = RING_BYTES - used - 1;

	if (bytes > free_bytes)
		return; /* reader is behind — drop this window, never block audio */

	unsigned int off = head % RING_BYTES;
	unsigned int first = RING_BYTES - off;
	if (first > bytes)
		first = bytes;
	memcpy(tap->ring + off, buf, first);
	if (bytes > first)
		memcpy(tap->ring, (const unsigned char *)buf + first, bytes - first);

	atomic_store_explicit(&tap->head, head + bytes, memory_order_release);
}

static unsigned int ring_get(stellar_tap_t *tap, unsigned char *out, unsigned int max)
{
	unsigned int tail = atomic_load_explicit(&tap->tail, memory_order_relaxed);
	unsigned int head = atomic_load_explicit(&tap->head, memory_order_acquire);
	unsigned int used = head - tail;

	if (used == 0)
		return 0;
	if (used > max)
		used = max;

	unsigned int off = tail % RING_BYTES;
	unsigned int first = RING_BYTES - off;
	if (first > used)
		first = used;
	memcpy(out, tap->ring + off, first);
	if (used > first)
		memcpy(out + first, tap->ring, used - first);

	atomic_store_explicit(&tap->tail, tail + used, memory_order_release);
	return used;
}

/* ----------------------------------------------------------- writer thread --*/

static void *writer_main(void *arg)
{
	stellar_tap_t *tap = arg;
	unsigned char buf[8192];
	int fd = -1;
	sigset_t pipeset;

	/* If the backend closes the read end, write() raises SIGPIPE — whose default
	 * disposition would terminate mpd. We must not install a process-wide handler
	 * inside someone else's process, so block it for THIS thread only: a blocked
	 * SIGPIPE with default disposition simply goes pending and never fires, and
	 * write() still returns EPIPE so the reopen path below works. The pending bit
	 * is drained after each EPIPE so it cannot mask a later diagnosis. */
	sigemptyset(&pipeset);
	sigaddset(&pipeset, SIGPIPE);
	pthread_sigmask(SIG_BLOCK, &pipeset, NULL);

	/* Pace output to the exact byte rate rather than draining the ring as fast as
	 * it fills.
	 *
	 * transfer() delivers one MPD period at a time, so a free-running writer emits
	 * in bursts. The backend infers the stream rate from FIFO arrival timing over a
	 * short window, and bursty arrival made it flap between 44100 and 48000 — 234
	 * remaps in 20 minutes, against 2 in 50 on the path this replaced. We know the
	 * true rate exactly, so meter it out on a monotonic clock and the estimator
	 * settles. Credit is capped so a pause cannot bank time and then burst. */
	struct timespec last;
	clock_gettime(CLOCK_MONOTONIC, &last);
	double credit = 0.0;

	while (atomic_load_explicit(&tap->writer_running, memory_order_acquire)) {
		struct timespec now;
		clock_gettime(CLOCK_MONOTONIC, &now);
		double dt = (double)(now.tv_sec - last.tv_sec) +
			    (double)(now.tv_nsec - last.tv_nsec) / 1e9;
		last = now;

		unsigned int byterate =
			atomic_load_explicit(&tap->out_byterate, memory_order_relaxed);
		if (byterate == 0) {
			usleep(5000);
			continue;
		}
		credit += dt * (double)byterate;
		if (credit > (double)sizeof(buf))
			credit = (double)sizeof(buf);
		if (credit < 64.0) {
			usleep(2000);
			continue;
		}

		unsigned int want = (unsigned int)credit;
		if (want > sizeof(buf))
			want = sizeof(buf);
		unsigned int n = ring_get(tap, buf, want);
		if (n == 0) {
			/* nothing staged — do not bank the idle time */
			credit = 0.0;
			usleep(5000);
			continue;
		}
		credit -= (double)n;
		if (fd < 0) {
			/* O_NONBLOCK: returns ENXIO rather than blocking when the
			 * backend is not reading yet. */
			fd = open(tap->fifo_path, O_WRONLY | O_NONBLOCK);
			if (fd < 0)
				continue; /* drop until a reader shows up */
		}
		ssize_t w = write(fd, buf, n);
		if (w < 0 && errno != EAGAIN && errno != EWOULDBLOCK && errno != EINTR) {
			if (errno == EPIPE) {
				/* consume the SIGPIPE we just made pending on this thread */
				struct timespec zero = { 0, 0 };
				sigtimedwait(&pipeset, NULL, &zero);
			}
			close(fd);
			fd = -1;
		}
	}
	if (fd >= 0)
		close(fd);
	return NULL;
}

/* -------------------------------------------------------------- conversion --*/

static inline const unsigned char *area_addr(const snd_pcm_channel_area_t *a,
					     snd_pcm_uframes_t frame)
{
	return (const unsigned char *)a->addr + (a->first + a->step * frame) / 8;
}

/* Feed the meter from PCM. Emitted at the source rate, unchanged from the
 * behaviour the s16 scope had, so the backend's rate estimator sees no change. */
static void feed_pcm(stellar_tap_t *tap, const snd_pcm_channel_area_t *areas,
		     snd_pcm_uframes_t offset, snd_pcm_uframes_t size)
{
	int16_t stage[512 * 2];
	unsigned int ch = tap->channels > 2 ? 2 : tap->channels;
	snd_pcm_uframes_t done = 0;

	while (done < size) {
		snd_pcm_uframes_t n = size - done;
		if (n > 512)
			n = 512;
		for (snd_pcm_uframes_t i = 0; i < n; i++) {
			for (unsigned int c = 0; c < ch; c++) {
				const unsigned char *p =
					area_addr(&areas[c], offset + done + i);
				int16_t v;
				if (tap->mode == TAP_MODE_S16) {
					int16_t s;
					memcpy(&s, p, 2);
					v = s;
				} else {
					int32_t s;
					memcpy(&s, p, 4);
					v = (int16_t)(s >> 16);
				}
				stage[i * 2 + c] = v;
			}
			if (ch == 1)
				stage[i * 2 + 1] = stage[i * 2];
		}
		ring_put(tap, stage, (unsigned int)(n * 2 * sizeof(int16_t)));
		done += n;
	}
}

/* Feed the meter from native DSD.
 *
 * DSD_U32_BE packs 32 one-bit samples per channel into each frame, MSB first.
 * A boxcar average of the bit density is a crude but perfectly adequate low-pass
 * for a level meter: density 0.5 is silence, and the first null lands at the
 * output rate. Decimating by dsd_decim frames yields ~DSD_TARGET_RATE samples/s. */
static void feed_dsd(stellar_tap_t *tap, const snd_pcm_channel_area_t *areas,
		     snd_pcm_uframes_t offset, snd_pcm_uframes_t size)
{
	int16_t stage[512 * 2];
	unsigned int staged = 0;
	unsigned int ch = tap->channels > 2 ? 2 : tap->channels;

	for (snd_pcm_uframes_t i = 0; i < size; i++) {
		for (unsigned int c = 0; c < ch; c++) {
			uint32_t w;
			memcpy(&w, area_addr(&areas[c], offset + i), 4);
			tap->dsd_count[c] += (unsigned int)__builtin_popcount(w);
		}
		tap->dsd_frames++;

		if (tap->dsd_frames < tap->dsd_decim)
			continue;

		unsigned int bits = tap->dsd_frames * 32u;
		for (unsigned int c = 0; c < ch; c++) {
			/* Density in [0,1] -> signed amplitude.
			 *
			 * The scale factor is 4, not 2. DSD's 0 dBFS reference is 50%
			 * MODULATION DEPTH (SACD/DSD-IEC), so a full-scale signal swings
			 * the bit density only +/-0.25 around 0.5 — halfway to the +/-0.5
			 * the encoding could physically reach. Using 2 would read every
			 * DSD track a uniform 6 dB below the equivalent PCM one and make
			 * the needles visibly lazier on DSD. Overshoot is possible above
			 * 0 dBFS modulation, hence the clamp. */
			double density = (double)tap->dsd_count[c] / (double)bits;
			double amp = (density - 0.5) * 4.0;
			if (amp > 1.0) amp = 1.0;
			if (amp < -1.0) amp = -1.0;
			stage[staged * 2 + c] = (int16_t)(amp * 32767.0);
			tap->dsd_count[c] = 0;
		}
		if (ch == 1)
			stage[staged * 2 + 1] = stage[staged * 2];
		tap->dsd_frames = 0;
		staged++;

		if (staged == 512) {
			ring_put(tap, stage, staged * 2 * (unsigned int)sizeof(int16_t));
			staged = 0;
		}
	}
	if (staged)
		ring_put(tap, stage, staged * 2 * (unsigned int)sizeof(int16_t));
}

/* --------------------------------------------------------------- callbacks --*/

static snd_pcm_sframes_t tap_transfer(snd_pcm_extplug_t *ext,
				      const snd_pcm_channel_area_t *dst_areas,
				      snd_pcm_uframes_t dst_offset,
				      const snd_pcm_channel_area_t *src_areas,
				      snd_pcm_uframes_t src_offset,
				      snd_pcm_uframes_t size)
{
	stellar_tap_t *tap = ext->private_data;

	/* The audio itself, first and unconditionally. */
	snd_pcm_areas_copy(dst_areas, dst_offset, src_areas, src_offset,
			   ext->channels, size, ext->format);

	switch (tap->mode) {
	case TAP_MODE_S16:
	case TAP_MODE_S32:
		feed_pcm(tap, src_areas, src_offset, size);
		break;
	case TAP_MODE_DSD_U32:
		feed_dsd(tap, src_areas, src_offset, size);
		break;
	case TAP_MODE_NONE:
		break;
	}
	return (snd_pcm_sframes_t)size;
}

static int tap_hw_params(snd_pcm_extplug_t *ext, snd_pcm_hw_params_t *params)
{
	stellar_tap_t *tap = ext->private_data;
	unsigned int out_rate;
	(void)params;

	tap->channels = ext->channels;
	tap->dsd_count[0] = tap->dsd_count[1] = 0;
	tap->dsd_frames = 0;
	out_rate = ext->rate;

	switch (ext->format) {
	case SND_PCM_FORMAT_S16_LE:
		tap->mode = TAP_MODE_S16;
		break;
	case SND_PCM_FORMAT_S32_LE:
		tap->mode = TAP_MODE_S32;
		break;
	case SND_PCM_FORMAT_DSD_U32_BE:
	case SND_PCM_FORMAT_DSD_U32_LE:
		tap->mode = TAP_MODE_DSD_U32;
		/* ext->rate is the DSD frame rate (bit rate / 32). */
		tap->dsd_decim = ext->rate / DSD_TARGET_RATE;
		if (tap->dsd_decim == 0)
			tap->dsd_decim = 1;
		out_rate = ext->rate / tap->dsd_decim;
		break;
	default:
		/* Big-endian PCM, S24_3LE, float, DSD_U8/U16: pass through with no
		 * meter rather than guess. Nothing here is reachable on this DAC. */
		tap->mode = TAP_MODE_NONE;
		out_rate = 0;
		break;
	}

	/* stereo S16 out, whatever came in */
	atomic_store_explicit(&tap->out_byterate, out_rate * 4u, memory_order_relaxed);
	return 0;
}

static int tap_init(snd_pcm_extplug_t *ext)
{
	stellar_tap_t *tap = ext->private_data;

	/* Drop anything staged for a previous stream. */
	atomic_store(&tap->tail, atomic_load(&tap->head));
	tap->dsd_count[0] = tap->dsd_count[1] = 0;
	tap->dsd_frames = 0;
	return 0;
}

static int tap_close(snd_pcm_extplug_t *ext)
{
	stellar_tap_t *tap = ext->private_data;

	if (tap->writer_started) {
		atomic_store_explicit(&tap->writer_running, 0, memory_order_release);
		pthread_join(tap->writer, NULL);
	}
	free(tap->ring);
	free(tap->fifo_path);
	free(tap);
	return 0;
}

static const snd_pcm_extplug_callback_t tap_callback = {
	.transfer  = tap_transfer,
	.close     = tap_close,
	.hw_params = tap_hw_params,
	.init      = tap_init,
};

/* --------------------------------------------------------------- open ------*/

SND_PCM_PLUGIN_DEFINE_FUNC(stellar_tap)
{
	snd_config_iterator_t i, next;
	snd_config_t *sconf = NULL;
	const char *fifo = DEFAULT_FIFO;
	stellar_tap_t *tap;
	int err;

	snd_config_for_each(i, next, conf) {
		snd_config_t *n = snd_config_iterator_entry(i);
		const char *id;
		if (snd_config_get_id(n, &id) < 0)
			continue;
		if (!strcmp(id, "comment") || !strcmp(id, "type") || !strcmp(id, "hint"))
			continue;
		if (!strcmp(id, "slave")) {
			sconf = n;
			continue;
		}
		if (!strcmp(id, "fifo")) {
			if (snd_config_get_string(n, &fifo) < 0) {
				SNDERR("stellar_tap: fifo must be a string");
				return -EINVAL;
			}
			continue;
		}
		SNDERR("stellar_tap: unknown field %s", id);
		return -EINVAL;
	}
	if (!sconf) {
		SNDERR("stellar_tap: no slave configuration");
		return -EINVAL;
	}

	tap = calloc(1, sizeof(*tap));
	if (!tap)
		return -ENOMEM;

	tap->fifo_path = strdup(fifo);
	tap->ring = malloc(RING_BYTES);
	if (!tap->fifo_path || !tap->ring) {
		free(tap->ring);
		free(tap->fifo_path);
		free(tap);
		return -ENOMEM;
	}

	/* Harmless if it already exists — a tmpfiles.d rule creates it at boot. */
	if (mkfifo(tap->fifo_path, 0666) < 0 && errno != EEXIST)
		SNDERR("stellar_tap: mkfifo %s: %s", tap->fifo_path, strerror(errno));

	tap->ext.version      = SND_PCM_EXTPLUG_VERSION;
	tap->ext.name         = "Stellar VU tap";
	tap->ext.callback     = &tap_callback;
	tap->ext.private_data = tap;

	err = snd_pcm_extplug_create(&tap->ext, name, root, sconf, stream, mode);
	if (err < 0) {
		free(tap->ring);
		free(tap->fifo_path);
		free(tap);
		return err;
	}

	/* Convert nothing: client format/channels are pinned to the slave's, so the
	 * DAC's own capability list is what a client negotiates against. */
	snd_pcm_extplug_set_param_link(&tap->ext, SND_PCM_EXTPLUG_HW_FORMAT, 1);
	snd_pcm_extplug_set_param_link(&tap->ext, SND_PCM_EXTPLUG_HW_CHANNELS, 1);

	atomic_store(&tap->writer_running, 1);
	if (pthread_create(&tap->writer, NULL, writer_main, tap) == 0) {
		tap->writer_started = 1;
	} else {
		atomic_store(&tap->writer_running, 0);
		SNDERR("stellar_tap: could not start writer thread; meter disabled");
	}

	*pcmp = tap->ext.pcm;
	return 0;
}

SND_PCM_PLUGIN_SYMBOL(stellar_tap);
