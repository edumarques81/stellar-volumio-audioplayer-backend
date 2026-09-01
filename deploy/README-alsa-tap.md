# The VU meter tap — `stellar_tap`

**Status: current.** Supersedes the `type meter` + scope design in
[`README-alsa-scope.md`](README-alsa-scope.md), which could not meter DSD.

## The constraint this exists to satisfy

The LCD VU meter needs a copy of the audio. The obvious way to get one — a second
MPD `audio_output` writing a FIFO — is architecturally wrong, not merely slow:

> MPD returns a decoded chunk to its shared `MusicBuffer` pool only once **every**
> enabled output has consumed it.

So a second output is a second consumer on the DAC's critical path *by construction*.
It is not tunable, and MPD misreports the resulting starvation as
`alsa_output: Decoder is too slow; playing silence to avoid xrun`. Measured at 192 kHz:

| Configuration | Result |
|---|---|
| FIFO output on, `buffer_time` removed | 1 XRUN in ~5 min |
| FIFO output on, buffer + `performance` governor | 1 XRUN in ~1 min |
| FIFO output disabled | 0 XRUN in 21 min |
| ALSA `type meter` + scope | 0 XRUN in 20 min |
| **`stellar_tap` extplug (this design)** | **0 XRUN in 20 min on DSD512** |

**Never add a second `audio_output`.**

## Why not the ALSA `type meter` plugin

That was the previous fix and it worked for PCM, but it cannot meter DSD, and the
failure is violent rather than graceful:

- The only public way a scope can read samples is
  `snd_pcm_scope_s16_get_channel_buffer()`.
- alsa-lib's s16 conversion scope returns `-EINVAL` for `DSD_U32_BE`
  (and S24_3LE, float, and S16-already-in-MMAP_NONINTERLEAVED).
- `snd_pcm_scope_enable()` records `scope->enabled = (err >= 0)` and then **discards
  the error**. The s16 scope is left disabled with `buf_areas == NULL` while our own
  scope stays enabled and keeps receiving `update()`.
- The next accessor call trips `assert(s16->buf_areas)` — which **aborts mpd
  mid-track**. Observed, not theorised.

Vendoring alsa-lib's `pcm_meter.c` to reach its raw buffer is also a dead end: **44 of
the 76** `snd_pcm_*` symbols it needs (`snd_pcm_new`, `snd_pcm_open_slave`,
`snd_pcm_hw_params_slave`, `_snd_pcm_hw_params_internal`, `snd_pcm_link_hw_ptr`,
`snd_pcm_mmap_areas`, `snd_pcm_linear_convert*`, …) are not exported by libasound.
A standalone vendored meter cannot link, whichever source version is pinned.

## The design

`snd_pcm_extplug` is a **public, exported, versioned** filter-plugin API. alsa-lib owns
the slave; we implement `transfer()` and receive the raw channel areas for *every*
format the card accepts, DSD included.

```
  before (broken)            after (this)
  ---------------            ------------
  mpd                        mpd
   ├── output 1 → hw:2,0      └── output 1 → stellar_dac_tap  (extplug)
   └── output 2 → FIFO   ✗                      │  areas_copy → hw:2,0
        ^ second consumer                       └─ ring → writer thread → FIFO
          of the chunk pool                          (off the audio thread)
```

`snd_pcm_extplug_set_param_link(…, SND_PCM_EXTPLUG_HW_FORMAT, 1)` pins the client
format to the slave's, so **nothing is converted** and the card's own capability list
is what a client negotiates against. Verified with `aplay --dump-hw-params`: the tap
advertises `S16_LE S32_LE SPECIAL DSD_U32_BE`, `RATE [44100 768000]`, and *rejects*
U8/8000/mono exactly as the bare card does. It is a tap, not a `plug` layer.

### Threading

`transfer()` runs on the caller's audio thread and does the minimum: one
`snd_pcm_areas_copy()` pass-through, then a bounded conversion into a lock-free SPSC
ring. A writer thread drains the ring into the FIFO. Nothing on the audio thread can
block — the ring drops on overflow, the FIFO write is `O_NONBLOCK` with drop-on-full.

`SIGPIPE` is blocked **thread-locally** in the writer (`pthread_sigmask`), never with a
process-wide handler: we are a guest inside mpd. A blocked `SIGPIPE` with default
disposition simply goes pending and never fires, while `write()` still returns `EPIPE`
so the reopen path works. The pending bit is drained after each `EPIPE`.

### Metering

| Slave format | Handling |
|---|---|
| `S16_LE` | copied straight out |
| `S32_LE` (incl. 24-in-32) | high 16 bits |
| `DSD_U32_BE` / `_LE` | popcount bit-density, decimated to ~44.1 kHz |
| anything else | passed through, not metered (unreachable on this DAC) |

DSD carries 32 one-bit samples per channel per frame. A boxcar average of the bit
density is a crude but adequate low-pass for a *level* meter; decimating by
`rate / 44100` frames (16 at DSD512) puts the first null at the output rate.

The scale factor is **4, not 2**: DSD's 0 dBFS reference is 50% *modulation depth*, so
a full-scale signal swings density only ±0.25 around 0.5. Using 2 reads every DSD track
a uniform 6 dB below the equivalent PCM one. Measured on the same album pair:

| Source | peak | rms | quietest 100 ms |
|---|---|---|---|
| 44.1/16 PCM | −0.0 dBFS | −15.6 | −32.3 |
| DSD512 (×4) | −2.1 dBFS | −22.6 | −39.3 |

## Build and install

Built **on the Pi** (aarch64) — it links against the Pi's own libasound.

```bash
sudo apt-get install -y libasound2-dev
cd deploy/alsa-tap && make && sudo make install
sudo systemctl restart mpd shairport-sync
```

### Trap: `-DPIC` is mandatory, and applies to every alsa-lib plugin

`SND_PCM_PLUGIN_SYMBOL()` expands to `SND_DLSYM_BUILD_VERSION`, and `<alsa/global.h>`
switches that on `#ifdef PIC`. The non-PIC branch roots a linked list at
`snd_dlsym_start`, an internal symbol libasound does **not** export. gcc defines
`__PIC__` for `-fPIC` but never `PIC`, so without `-DPIC` the module compiles and links
cleanly and then fails at `dlopen`. The symptom is indirect and misleading:

```
ALSA lib dlmisc.c:337: Cannot open shared library …: undefined symbol: snd_dlsym_start
mpd: Failed to open ALSA device "stellar_dac_tap": No such device or address
```

## Configuration

`/etc/asound.conf`:

```
pcm.stellar_dac_tap {
    type  stellar_tap
    slave.pcm "hw:2,0"
    fifo  "/tmp/mpd_spectrum.fifo"
}
```

`/etc/mpd.conf` — one output, pointed at the tap, `mixer_type "none"`:

```
audio_output {
    type        "alsa"
    name        "USB DAC"
    device      "stellar_dac_tap"
    mixer_type  "none"
}
```

`/etc/shairport-sync.conf` — `output_device = "stellar_dac_tap";` so AirPlay meters
through the same path.

The FIFO is created at boot by `/etc/tmpfiles.d/stellar-spectrum-fifo.conf`
(`p /tmp/mpd_spectrum.fifo 0666 mpd audio -`). Without it, whichever of mpd/backend
starts first creates it with the wrong owner and the meter is silently dead until a
manual restart. The plugin also `mkfifo`s defensively.

MPD caches the ALSA configuration at startup — **`/etc/asound.conf` edits need an mpd
restart**, not just a track change.

## Verification

```bash
# bit-perfect passthrough: must equal the source, never a converted rate
awk -F: '/^format|^rate/' /proc/asound/card2/pcm0p/sub0/hw_params
```

| Source | Expected `hw_params` |
|---|---|
| 44.1 kHz 16-bit | `S16_LE` @ 44100 |
| 96 / 192 / 352.8 kHz 24-bit | `S32_LE` at the source rate |
| DSD512 | `DSD_U32_BE` @ 705600 |

The backend logs `[Spectrum] FIFO … streaming` and a `FIFO sample rate is N Hz` line;
on DSD that N is the decimated 44100, not 705600.

MPD does **not** log ALSA XRUNs. Detect them by polling
`/proc/asound/card2/pcm0p/sub0/status` for `*XRUN*`. Note the field format is
`avail_max   : 4034` — spaces *before* the colon, so `/^avail_max:/` will not match;
use `awk -F: '/avail_max/{gsub(/[ \t]/,"",$2);print $2}'`.

## Maintenance

- A `libasound2` upgrade means **rebuild and reinstall**. A stale or missing module does
  not degrade to "no VU meter" — mpd fails to open the device, so the symptom is
  **no sound at all**.
- Reverting is one word: point `/etc/mpd.conf` at `hw:2,0` (no meter, no VU) or at the
  retired `stellar_dac` meter chain, which is still defined in `/etc/asound.conf`.
