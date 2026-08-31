# The VU meter tap — ALSA `type meter`, not a second MPD output

The LCD VU meter needs a copy of the audio. The obvious way to get one is a second
`audio_output` in `/etc/mpd.conf` writing to a FIFO. **That way is wrong**, and it
cost two rounds of investigation to establish why.

## Why a second MPD output cannot work

MPD returns a decoded chunk to its shared `MusicBuffer` pool only once *every*
enabled output has consumed it (`MultipleOutputs`). A second output is therefore a
second consumer sitting **on the DAC's critical path by construction**. Whatever it
does — resample, block, merely run late — it can stall the decoder feeding the DAC.
MPD then blames the wrong component:

```
alsa_output: Decoder is too slow; playing silence to avoid xrun
```

The 2026-08-26 fix (`format "*:16:2"`, removing a resampler from that output's
filter chain) was real and large: the `Decoder is too slow` line went to zero and
dropouts fell roughly 10x. It was **not** a solution. A residual ~1 XRUN per
5-10 min survived, because the coupling itself is not a tuning problem.

Measured on 2026-08-31 at 192 kHz, with the resampler already gone:

| Config | Result |
|---|---|
| FIFO output enabled, `audio_buffer_size` removed | 1 XRUN in ~5 min |
| FIFO output enabled, buffer restored + `performance` governor | 1 XRUN in ~1 min |
| **FIFO output disabled** | **0 XRUN in 21 min / 4 tracks** |

Also rejected by measurement, do not retry: pinning or demoting the `output:Spectrum`
thread (made it *worse* — a slower consumer stalls `player` longer), a cheaper
`samplerate_converter`, and a larger `audio_buffer_size`.

## What replaces it

ALSA's `type meter` PCM is a transparent pass-through: it forwards identical samples
to its slave device and hands each buffer to "scope" callbacks on the side. MPD keeps
**one** output. There is no second consumer and no chunk pool involved. This is the
same mechanism moOde/Volumio use for PeppyMeter, and it is already compiled into
libasound 1.2.8 on the Pi — nothing to rebuild.

Better still, scope `update()` runs on the meter's own thread (`snd_pcm_meter_thread`,
`nanosleep`-paced), so the FIFO write is not on MPD's audio path at all. The write is
additionally `O_NONBLOCK` with drop-on-full, so a stalled or absent reader cannot
apply back-pressure even in principle.

```
       before                              after
  ┌──────────────┐                   ┌──────────────┐
  │     MPD      │                   │     MPD      │
  └──┬────────┬──┘                   └──────┬───────┘
     │        │  shared chunk pool           │   one output
  ┌──▼──┐  ┌──▼──────┐                ┌──────▼───────┐
  │ DAC │  │FIFO out │ ← stalls DAC   │ pcm.stellar_ │
  └─────┘  └─────────┘                │ dac (meter)  │
                                      └──┬────────┬──┘
                                         │        │ meter thread
                                    ┌────▼─┐  ┌───▼────────┐
                                    │ DAC  │  │stellar_vu  │→ FIFO
                                    └──────┘  │  scope     │
                                              └────────────┘
```

## Build and install

Build **on the Pi** (aarch64). This is the one component that is not cross-compiled:
alsa-lib's scope ABI is version-sensitive, so it links against the Pi's own libasound.

```bash
sudo apt-get install -y libasound2-dev
scp -r deploy/alsa-scope eduardo@stellar.local:/tmp/
ssh eduardo@stellar.local 'cd /tmp/alsa-scope && make && sudo make install'
```

`make install` drops `libasound_module_scope_stellar_vu.so` into
`/usr/lib/aarch64-linux-gnu/alsa-lib/`.

Also install the tmpfiles rule, which creates the FIFO node at boot:

```bash
sudo cp deploy/alsa-scope/stellar-spectrum-fifo.conf /etc/tmpfiles.d/
sudo systemd-tmpfiles --create /etc/tmpfiles.d/stellar-spectrum-fifo.conf
```

Without it the node does not exist between a reboot and the first track played —
`/tmp` is a tmpfs and the scope only `mkfifo()`s on first PCM open — so the
backend's spectrum reader retries every 2s and logs a failure each time. The old
MPD `fifo` output used to create the node at startup; nothing does now.

## Configuration

`/etc/asound.conf` — **all three stanzas are required.** Unlike PCM plugins, the meter
does *not* derive a scope's `.so` filename from its `type`; an inline
`scopes.0 { type stellar_vu ... }` fails with
`symbol _snd_pcm_scope_stellar_vu_open is not defined inside (null)`.

```
pcm_scope_type.stellar_vu {
    lib "/usr/lib/aarch64-linux-gnu/alsa-lib/libasound_module_scope_stellar_vu.so"
}
pcm_scope.stellar_vu {
    type stellar_vu
    fifo "/tmp/mpd_spectrum.fifo"
}
pcm.stellar_dac {
    type meter
    slave.pcm "hw:2,0"
    scopes.0 stellar_vu
}
```

`/etc/mpd.conf` — the DAC output points at the meter instead of the raw device, and
there is **no** second output:

```
audio_output {
    type        "alsa"
    name        "USB DAC"
    device      "stellar_dac"
    mixer_type  "none"
}
```

MPD caches the ALSA configuration at startup, so any `asound.conf` edit needs
`sudo systemctl restart mpd`.

## Two traps worth knowing before you touch the plugin

**`-DPIC` is load-bearing, not decoration.** `<alsa/global.h>` selects between two
`SND_DLSYM_BUILD_VERSION` definitions on `#ifdef PIC`. The static branch builds a
linked list rooted at `snd_dlsym_start` — an internal symbol libasound does not
export. gcc defines `__PIC__` for `-fPIC` but *never* `PIC`, so without `-DPIC` the
module compiles and links cleanly and then fails at dlopen with
`undefined symbol: snd_dlsym_start`.

**alsa-lib's s16 scope will abort mpd on DSD if you let it.**
`snd_pcm_scope_enable()` records `scope->enabled = (err >= 0)` and then discards the
error. When alsa-lib's `s16_enable()` hits its `default: return -EINVAL` branch — DSD,
S24_3LE, float — the s16 scope is left disabled with `buf_areas == NULL` while *our*
scope stays enabled and keeps receiving `update()`. The next
`snd_pcm_scope_s16_get_channel_buffer()` trips `assert(s16->buf_areas)` and kills the
process mid-track:

```
mpd: pcm_meter.c:1220: snd_pcm_scope_s16_get_channel_buffer: Assertion `s16->buf_areas' failed.
```

There is no public way to ask whether another scope enabled, so `stellar_vu_scope.c`
re-derives the answer from the format in its own `enable()` and declines the same
cases s16 would. Declining is the correct outcome: DSD keeps playing bit-perfect with
the meter simply idle, and `enable()` re-runs per `hw_params`, so the next PCM track
re-arms the tap by itself. **If you edit `stellar_vu_format_meterable()`, keep it in
sync with `s16_enable()` in alsa-lib's `src/pcm/pcm_meter.c`.**

## Verification

Bit-perfect is preserved — the meter advertises the card's own capabilities rather
than a `plug` layer's:

```bash
aplay -D stellar_dac --dump-hw-params /dev/zero   # advertises S16_LE S32_LE DSD_U32_BE,
                                                  # RATE [44100 768000]; rejects U8/8000/mono
cat /proc/asound/card2/pcm0p/sub0/hw_params       # while playing
```

Every format in the library, verified 2026-08-31, all with zero MPD errors:

| Source | `hw_params` |
|---|---|
| 44.1 kHz / 16 bit | `S16_LE @ 44100` |
| 44.1 / 48 / 96 / 176.4 / 192 / 352.8 kHz, 24 bit | `S32_LE` at the source rate |
| DSD512 `.dsf` | `DSD_U32_BE @ 705600` |

No resampling and no forced bit-depth conversion at any rate.

## Maintenance

The plugin links against the Pi's own libasound, and alsa-lib's scope ABI is not a
stable public contract. **After any `libasound2` package upgrade, rebuild and
reinstall** (`cd deploy/alsa-scope && make && sudo make install`), then restart mpd.
A stale module fails at dlopen and MPD falls back to erroring on the device, so the
symptom is "no sound at all", not a silent VU meter.

`/etc/asound.conf` and the `.so` are both outside `/home`, so neither is captured by
a home-directory backup. They are reproduced by this directory plus the config block
above.
