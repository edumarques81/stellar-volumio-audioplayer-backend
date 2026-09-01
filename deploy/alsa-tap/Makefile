# Build the stellar_tap ALSA extplug plugin. Build ON THE PI (aarch64).
#
#   sudo apt-get install -y libasound2-dev
#   make && sudo make install
#
# This uses only snd_pcm_extplug — a public, exported, versioned API — so unlike
# the retired scope plugin it is not tied to alsa-lib's internal struct layout.
#
# -DPIC is still load-bearing, and applies to EVERY alsa-lib plugin, extplug
# included: SND_PCM_PLUGIN_SYMBOL() expands to SND_DLSYM_BUILD_VERSION, and
# <alsa/global.h> switches that on `#ifdef PIC`. The non-PIC branch builds a
# linked list rooted at `snd_dlsym_start`, an internal symbol libasound does NOT
# export. gcc defines __PIC__ for -fPIC but never PIC, so without -DPIC the module
# compiles and links cleanly, then fails at dlopen with
# "undefined symbol: snd_dlsym_start" — which surfaces only as mpd reporting
# "Failed to open ALSA device: No such device or address".

LIBDIR ?= $(shell pkg-config --variable=libdir alsa)/alsa-lib
CFLAGS ?= -O2 -Wall -Wextra -fPIC -DPIC -std=gnu11
LDLIBS := $(shell pkg-config --libs alsa) -lpthread

TARGET := libasound_module_pcm_stellar_tap.so

.PHONY: all clean install uninstall
all: $(TARGET)
$(TARGET): stellar_tap.c
	$(CC) $(CFLAGS) -shared -o $@ $< $(LDLIBS)
install: $(TARGET)
	install -D -m 0644 $(TARGET) $(DESTDIR)$(LIBDIR)/$(TARGET)
uninstall:
	rm -f $(DESTDIR)$(LIBDIR)/$(TARGET)
clean:
	rm -f $(TARGET)
