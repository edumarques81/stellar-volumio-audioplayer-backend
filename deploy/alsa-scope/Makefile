# Build the stellar_vu ALSA scope plugin. Build ON THE PI (aarch64) — this is
# the one component we do not cross-compile, because it links against the Pi's
# own libasound and alsa-lib's scope ABI is version-sensitive.
#
#   sudo apt-get install -y libasound2-dev
#   make && sudo make install
#
# `make install` drops the .so next to the other alsa-lib modules; the meter
# plugin finds it by name (`type stellar_vu` -> libasound_module_scope_stellar_vu.so).

LIBDIR ?= $(shell pkg-config --variable=libdir alsa)/alsa-lib

# -DPIC is load-bearing, not decoration. <alsa/global.h> selects between two
# SND_DLSYM_BUILD_VERSION definitions on `#ifdef PIC`: the shared-object one
# emits a plain `char _<sym>_dlsym_pcm_001;`, while the static one builds a
# linked list rooted at `snd_dlsym_start` — an internal symbol libasound does
# NOT export. gcc defines __PIC__ for -fPIC but never PIC, so without -DPIC the
# module compiles and links cleanly and then fails at dlopen with
# "undefined symbol: snd_dlsym_start". alsa-lib's own build passes -DPIC.
CFLAGS ?= -O2 -Wall -Wextra -fPIC -DPIC -std=gnu11
LDLIBS := $(shell pkg-config --libs alsa)

TARGET := libasound_module_scope_stellar_vu.so

.PHONY: all clean install uninstall

all: $(TARGET)

$(TARGET): stellar_vu_scope.c
	$(CC) $(CFLAGS) -shared -o $@ $< $(LDLIBS)

install: $(TARGET)
	install -D -m 0644 $(TARGET) $(DESTDIR)$(LIBDIR)/$(TARGET)

uninstall:
	rm -f $(DESTDIR)$(LIBDIR)/$(TARGET)

clean:
	rm -f $(TARGET)
