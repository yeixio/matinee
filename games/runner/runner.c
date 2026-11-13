// SPDX-License-Identifier: MIT
//
// matinee-game-runner plays one game with one libretro emulator core and
// hands every frame and every bit of sound to Matinee, which encodes and
// streams them. It runs as its own process, so a core that crashes takes
// down only its own game, and so cores under any licence can be loaded
// without being linked into the server. It's MIT, like libretro.h, so it can
// load GPL cores.
//
//   matinee-game-runner <core.so> <game file or -> <save dir> <system dir>
//   matinee-game-runner --info <core.so>
//
// --info prints what the emulator says about itself, as JSON, and exits:
// {"name":…,"version":…,"extensions":[…],"needFullpath":…}. Matinee uses it
// to check an emulator someone added and to learn which games it plays.
//
// The emulator's settings (core options) keep their defaults, except those
// in MATINEE_GAME_OPTIONS: one key=value a line.
//
// Output (stdout) is a stream of messages, each an 8-byte header (a type
// letter, 3 bytes of padding, then the payload's length as a little-endian
// uint32) and its payload:
//
//   'I'  JSON: {"width":W,"height":H,"fps":F,"sampleRate":R,"aspect":A}
//        Sent once, before any frame. Every frame is W×H.
//   'V'  one frame, W×H pixels of 4 bytes, B G R X (FFmpeg's bgr0).
//   'A'  sound, 16-bit little-endian stereo samples at sampleRate.
//   'E'  text: why the game couldn't start. The runner then exits.
//
// Input (stdin) is a stream of 4-byte messages:
//
//   'B' port lo hi   buttons held on a controller, a bitmask of libretro's
//                    RETRO_DEVICE_ID_JOYPAD_* (B=0 … R3=15)
//   'X' port axis v  an analog stick: axis 0/1 the left stick's x/y, 2/3 the
//                    right's, v a signed byte (-128 left/up … 127 right/down)
//   'S' 0 0 0        write the game's save now
//
// When stdin closes, the save is written and the runner exits.

#define _GNU_SOURCE
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <math.h>
#include <poll.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

#include "libretro.h"

#define MAX_PORTS 4

static struct {
  void (*init)(void);
  void (*deinit)(void);
  void (*get_system_info)(struct retro_system_info *);
  void (*get_system_av_info)(struct retro_system_av_info *);
  void (*set_environment)(retro_environment_t);
  void (*set_video_refresh)(retro_video_refresh_t);
  void (*set_audio_sample)(retro_audio_sample_t);
  void (*set_audio_sample_batch)(retro_audio_sample_batch_t);
  void (*set_input_poll)(retro_input_poll_t);
  void (*set_input_state)(retro_input_state_t);
  void (*set_controller_port_device)(unsigned, unsigned);
  bool (*load_game)(const struct retro_game_info *);
  void (*unload_game)(void);
  void (*run)(void);
  void *(*get_memory_data)(unsigned);
  size_t (*get_memory_size)(unsigned);
} core;

static enum retro_pixel_format pixel_format = RETRO_PIXEL_FORMAT_0RGB1555;
static const char *save_dir = ".", *system_dir = ".";
static char save_path[4096];

static unsigned out_w, out_h;
static uint8_t *frame;         // out_w × out_h × 4, the last frame shown
static int16_t *sound;         // sound gathered while a frame runs
static size_t sound_len, sound_cap;
static uint16_t buttons[MAX_PORTS];
static int8_t axes[MAX_PORTS][4];
static uint8_t *last_save;     // the save as last written, to spot changes
static size_t last_save_len;

// ---- output ----

// out is where messages go: stdout, moved aside in main so that whatever an
// emulator prints (MAME does) lands in stderr and not among the messages.
static int out = 1;

static void write_all(const void *p, size_t n) {
  const uint8_t *b = p;
  while (n > 0) {
    ssize_t w = write(out, b, n);
    if (w < 0) {
      if (errno == EINTR) continue;
      exit(0);  // Matinee went away: nothing left to play for
    }
    b += w;
    n -= (size_t)w;
  }
}

static void send_msg(char type, const void *p, uint32_t n) {
  uint8_t h[8] = {(uint8_t)type, 0, 0, 0, n & 0xff, (n >> 8) & 0xff, (n >> 16) & 0xff, n >> 24};
  write_all(h, 8);
  if (n) write_all(p, n);
}

static void fail(const char *fmt, ...) {
  char msg[1024];
  va_list ap;
  va_start(ap, fmt);
  vsnprintf(msg, sizeof msg, fmt, ap);
  va_end(ap);
  send_msg('E', msg, (uint32_t)strlen(msg));
  exit(1);
}

// ---- libretro callbacks ----

static void core_log(enum retro_log_level level, const char *fmt, ...) {
  if (level < RETRO_LOG_WARN) return;
  va_list ap;
  va_start(ap, fmt);
  vfprintf(stderr, fmt, ap);
  va_end(ap);
}

// ---- core options ----

#define MAX_OPTIONS 1024
static struct {
  char *key, *value;
} options[MAX_OPTIONS];
static size_t n_options;

static void set_option(const char *key, const char *value, bool keep) {
  if (!key || !value) return;
  for (size_t i = 0; i < n_options; i++) {
    if (strcmp(options[i].key, key) == 0) {
      if (!keep) {
        free(options[i].value);
        options[i].value = strdup(value);
      }
      return;
    }
  }
  if (n_options == MAX_OPTIONS) return;
  options[n_options].key = strdup(key);
  options[n_options].value = strdup(value);
  n_options++;
}

// Settings from MATINEE_GAME_OPTIONS are set first, so the core's own
// defaults, which come later, don't replace them.
static void read_option_overrides(void) {
  const char *env = getenv("MATINEE_GAME_OPTIONS");
  if (!env) return;
  char *copy = strdup(env), *save = NULL;
  for (char *line = strtok_r(copy, "\n", &save); line; line = strtok_r(NULL, "\n", &save)) {
    char *eq = strchr(line, '=');
    if (!eq) continue;
    *eq = 0;
    set_option(line, eq + 1, false);
  }
  free(copy);
}

static void option_default(const char *key, const char *def, const struct retro_core_option_value *values) {
  if (!def || !*def) def = values[0].value;
  set_option(key, def, true);
}

static void set_options_v1(const struct retro_core_option_definition *d) {
  for (; d && d->key; d++) option_default(d->key, d->default_value, d->values);
}

static void set_options_v2(const struct retro_core_options_v2 *o) {
  if (!o) return;
  for (const struct retro_core_option_v2_definition *d = o->definitions; d && d->key; d++)
    option_default(d->key, d->default_value, d->values);
}

// The oldest way: "Description; first|second|third", the first the default.
static void set_variables(const struct retro_variable *v) {
  for (; v && v->key; v++) {
    const char *semi = v->value ? strchr(v->value, ';') : NULL;
    if (!semi) continue;
    semi++;
    while (*semi == ' ') semi++;
    char def[256];
    size_t n = strcspn(semi, "|");
    if (n >= sizeof def) n = sizeof def - 1;
    memcpy(def, semi, n);
    def[n] = 0;
    set_option(v->key, def, true);
  }
}

static bool environment(unsigned cmd, void *data) {
  switch (cmd) {
  case RETRO_ENVIRONMENT_GET_CAN_DUPE:
    *(bool *)data = true;
    return true;
  case RETRO_ENVIRONMENT_SET_PIXEL_FORMAT: {
    enum retro_pixel_format f = *(enum retro_pixel_format *)data;
    if (f > RETRO_PIXEL_FORMAT_RGB565) return false;
    pixel_format = f;
    return true;
  }
  case RETRO_ENVIRONMENT_GET_SYSTEM_DIRECTORY:
    *(const char **)data = system_dir;
    return true;
  case RETRO_ENVIRONMENT_GET_SAVE_DIRECTORY:
    *(const char **)data = save_dir;
    return true;
  case RETRO_ENVIRONMENT_GET_LOG_INTERFACE:
    ((struct retro_log_callback *)data)->log = core_log;
    return true;
  case RETRO_ENVIRONMENT_GET_VARIABLE: {
    struct retro_variable *v = data;
    v->value = NULL;
    for (size_t i = 0; v->key && i < n_options; i++) {
      if (strcmp(options[i].key, v->key) == 0) {
        v->value = options[i].value;
        return true;
      }
    }
    return false;
  }
  case RETRO_ENVIRONMENT_SET_VARIABLES:
    set_variables(data);
    return true;
  case RETRO_ENVIRONMENT_SET_CORE_OPTIONS:
    set_options_v1(data);
    return true;
  case RETRO_ENVIRONMENT_SET_CORE_OPTIONS_INTL:
    set_options_v1(data ? ((const struct retro_core_options_intl *)data)->us : NULL);
    return true;
  case RETRO_ENVIRONMENT_SET_CORE_OPTIONS_V2:
    set_options_v2(data);
    return true;
  case RETRO_ENVIRONMENT_SET_CORE_OPTIONS_V2_INTL:
    set_options_v2(data ? ((const struct retro_core_options_v2_intl *)data)->us : NULL);
    return true;
  case RETRO_ENVIRONMENT_SET_CORE_OPTIONS_DISPLAY:
  case RETRO_ENVIRONMENT_SET_CORE_OPTIONS_UPDATE_DISPLAY_CALLBACK:
    return true;
  case RETRO_ENVIRONMENT_GET_VARIABLE_UPDATE:
    *(bool *)data = false;
    return true;
  case RETRO_ENVIRONMENT_GET_INPUT_BITMASKS:
    return true;
  case RETRO_ENVIRONMENT_GET_CORE_OPTIONS_VERSION:
    *(unsigned *)data = 2;
    return true;
  case RETRO_ENVIRONMENT_SET_SUPPORT_NO_GAME:
  case RETRO_ENVIRONMENT_SET_INPUT_DESCRIPTORS:
  case RETRO_ENVIRONMENT_SET_CONTROLLER_INFO:
  case RETRO_ENVIRONMENT_SET_GEOMETRY:
    // Frames are scaled to the size sent at the start, so a new size
    // needs nothing more.
    return true;
  case RETRO_ENVIRONMENT_GET_LANGUAGE:
    *(unsigned *)data = RETRO_LANGUAGE_ENGLISH;
    return true;
  default:
    return false;
  }
}

static inline uint32_t pixel(const uint8_t *row, unsigned x) {
  switch (pixel_format) {
  case RETRO_PIXEL_FORMAT_XRGB8888:
    return ((const uint32_t *)row)[x];
  case RETRO_PIXEL_FORMAT_RGB565: {
    uint16_t p = ((const uint16_t *)row)[x];
    uint32_t r = (p >> 11) & 0x1f, g = (p >> 5) & 0x3f, b = p & 0x1f;
    return ((r << 3 | r >> 2) << 16) | ((g << 2 | g >> 4) << 8) | (b << 3 | b >> 2);
  }
  default: {
    uint16_t p = ((const uint16_t *)row)[x];
    uint32_t r = (p >> 10) & 0x1f, g = (p >> 5) & 0x1f, b = p & 0x1f;
    return ((r << 3 | r >> 2) << 16) | ((g << 3 | g >> 2) << 8) | (b << 3 | b >> 2);
  }
  }
}

// video copies the core's frame into frame, scaled (nearest pixel) when the
// core changed size since the start. NULL means the same frame again.
static void video(const void *data, unsigned w, unsigned h, size_t pitch) {
  if (!data || !w || !h) return;
  const uint8_t *src = data;
  uint32_t *dst = (uint32_t *)frame;
  for (unsigned y = 0; y < out_h; y++) {
    const uint8_t *row = src + (size_t)(y * h / out_h) * pitch;
    if (w == out_w) {
      for (unsigned x = 0; x < out_w; x++) dst[x] = pixel(row, x);
    } else {
      for (unsigned x = 0; x < out_w; x++) dst[x] = pixel(row, x * w / out_w);
    }
    dst += out_w;
  }
}

static void grow_sound(size_t more) {
  if (sound_len + more <= sound_cap) return;
  sound_cap = (sound_len + more) * 2;
  sound = realloc(sound, sound_cap * sizeof *sound);
  if (!sound) exit(1);
}

static void audio_sample(int16_t l, int16_t r) {
  grow_sound(2);
  sound[sound_len++] = l;
  sound[sound_len++] = r;
}

static size_t audio_batch(const int16_t *data, size_t frames) {
  grow_sound(frames * 2);
  memcpy(sound + sound_len, data, frames * 2 * sizeof *data);
  sound_len += frames * 2;
  return frames;
}

static void input_poll(void) {}

static int16_t input_state(unsigned port, unsigned device, unsigned index, unsigned id) {
  if (port >= MAX_PORTS) return 0;
  if ((device & RETRO_DEVICE_MASK) == RETRO_DEVICE_ANALOG) {
    if (index > RETRO_DEVICE_INDEX_ANALOG_RIGHT || id > RETRO_DEVICE_ID_ANALOG_Y) return 0;
    int v = axes[port][index * 2 + id];
    return (int16_t)(v < 0 ? v * 256 : v * 258);  // -32768 … 32766
  }
  if ((device & RETRO_DEVICE_MASK) != RETRO_DEVICE_JOYPAD) return 0;
  if (id == RETRO_DEVICE_ID_JOYPAD_MASK) return (int16_t)buttons[port];
  if (id > 15) return 0;
  return (buttons[port] >> id) & 1;
}

// ---- input ----

static void write_save(void);

// read_input applies whatever Matinee sent since the last frame. It
// returns false once Matinee has closed stdin.
static bool read_input(void) {
  static uint8_t buf[256];
  static size_t have;
  for (;;) {
    struct pollfd p = {.fd = 0, .events = POLLIN};
    if (poll(&p, 1, 0) <= 0) return true;
    ssize_t n = read(0, buf + have, sizeof buf - have);
    if (n == 0) return false;
    if (n < 0) return errno == EINTR || errno == EAGAIN;
    have += (size_t)n;
    size_t i = 0;
    for (; i + 4 <= have; i += 4) {
      uint8_t *m = buf + i;
      if (m[0] == 'B' && m[1] < MAX_PORTS) buttons[m[1]] = (uint16_t)(m[2] | m[3] << 8);
      else if (m[0] == 'X' && m[1] < MAX_PORTS && m[2] < 4) axes[m[1]][m[2]] = (int8_t)m[3];
      else if (m[0] == 'S') write_save();
    }
    memmove(buf, buf + i, have - i);
    have -= i;
  }
}

// ---- saves ----

static void read_save(void) {
  if (!core.get_memory_size || !save_path[0]) return;
  size_t n = core.get_memory_size(RETRO_MEMORY_SAVE_RAM);
  uint8_t *mem = core.get_memory_data(RETRO_MEMORY_SAVE_RAM);
  if (!n || !mem) return;
  FILE *f = fopen(save_path, "rb");
  if (f) {
    size_t got = fread(mem, 1, n, f);
    (void)got;
    fclose(f);
  }
  last_save = malloc(n);
  if (last_save) {
    memcpy(last_save, mem, n);
    last_save_len = n;
  }
}

// write_save writes the game's battery save, when it changed, to a new file
// that then takes the old one's place, so a crash never leaves half a save.
static void write_save(void) {
  if (!core.get_memory_size || !save_path[0]) return;
  size_t n = core.get_memory_size(RETRO_MEMORY_SAVE_RAM);
  uint8_t *mem = core.get_memory_data(RETRO_MEMORY_SAVE_RAM);
  if (!n || !mem) return;
  if (last_save && last_save_len == n && memcmp(last_save, mem, n) == 0) return;
  char tmp[4200];
  snprintf(tmp, sizeof tmp, "%s.new", save_path);
  FILE *f = fopen(tmp, "wb");
  if (!f) return;
  bool ok = fwrite(mem, 1, n, f) == n;
  ok = fflush(f) == 0 && fsync(fileno(f)) == 0 && ok;
  fclose(f);
  if (!ok || rename(tmp, save_path) != 0) {
    unlink(tmp);
    return;
  }
  free(last_save);
  last_save = malloc(n);
  if (last_save) {
    memcpy(last_save, mem, n);
    last_save_len = n;
  }
}

// ---- main ----

static void *sym(void *lib, const char *name) {
  void *p = dlsym(lib, name);
  if (!p) fail("the emulator has no %s", name);
  return p;
}

static uint8_t *read_file(const char *path, size_t *len) {
  FILE *f = fopen(path, "rb");
  if (!f) return NULL;
  struct stat st;
  if (fstat(fileno(f), &st) != 0 || st.st_size < 0) {
    fclose(f);
    return NULL;
  }
  uint8_t *data = malloc(st.st_size ? (size_t)st.st_size : 1);
  size_t got = data ? fread(data, 1, (size_t)st.st_size, f) : 0;
  fclose(f);
  if (!data || got != (size_t)st.st_size) {
    free(data);
    return NULL;
  }
  *len = got;
  return data;
}

static double now(void) {
  struct timespec t;
  clock_gettime(CLOCK_MONOTONIC, &t);
  return (double)t.tv_sec + (double)t.tv_nsec / 1e9;
}

static void sleep_until(double t) {
#ifdef __APPLE__
  /* macOS has no clock_nanosleep: sleep for what's left instead. */
  double left = t - now();
  if (left <= 0) return;
  struct timespec ts = {.tv_sec = (time_t)left, .tv_nsec = (long)((left - (double)(time_t)left) * 1e9)};
  while (nanosleep(&ts, &ts) == -1 && errno == EINTR) {
  }
#else
  struct timespec ts = {.tv_sec = (time_t)t, .tv_nsec = (long)((t - (double)(time_t)t) * 1e9)};
  while (clock_nanosleep(CLOCK_MONOTONIC, TIMER_ABSTIME, &ts, NULL) == EINTR) {
  }
#endif
}

static void json_string(const char *s) {
  putchar('"');
  for (; s && *s; s++) {
    unsigned char c = (unsigned char)*s;
    if (c == '"' || c == '\\') printf("\\%c", c);
    else if (c < 0x20) printf("\\u%04x", c);
    else putchar(c);
  }
  putchar('"');
}

// info prints what the core at path says about itself (see the top).
static int info(const char *path) {
  void *lib = dlopen(path, RTLD_NOW | RTLD_LOCAL);
  if (!lib) {
    fprintf(stderr, "%s\n", dlerror());
    return 1;
  }
  void (*get_system_info)(struct retro_system_info *) = dlsym(lib, "retro_get_system_info");
  unsigned (*api_version)(void) = dlsym(lib, "retro_api_version");
  if (!get_system_info || !api_version || !dlsym(lib, "retro_run") || !dlsym(lib, "retro_load_game")) {
    fprintf(stderr, "not a libretro core\n");
    return 1;
  }
  if (api_version() != RETRO_API_VERSION) {
    fprintf(stderr, "libretro API version %u, not %u\n", api_version(), RETRO_API_VERSION);
    return 1;
  }
  struct retro_system_info sys = {0};
  get_system_info(&sys);
  printf("{\"name\":");
  json_string(sys.library_name);
  printf(",\"version\":");
  json_string(sys.library_version);
  printf(",\"extensions\":[");
  bool first = true;
  if (sys.valid_extensions) {
    char *exts = strdup(sys.valid_extensions), *save = NULL;
    for (char *e = strtok_r(exts, "|", &save); e; e = strtok_r(NULL, "|", &save)) {
      if (!first) putchar(',');
      first = false;
      json_string(e);
    }
    free(exts);
  }
  printf("],\"needFullpath\":%s}\n", sys.need_fullpath ? "true" : "false");
  return 0;
}

int main(int argc, char **argv) {
  if (argc == 3 && strcmp(argv[1], "--info") == 0) return info(argv[2]);
  if (argc != 5) {
    fprintf(stderr, "usage: %s <core.so> <game file or -> <save dir> <system dir>\n       %s --info <core.so>\n", argv[0], argv[0]);
    return 2;
  }
  const char *core_path = argv[1], *game = argv[2];
  save_dir = argv[3];
  system_dir = argv[4];
  fcntl(0, F_SETFL, fcntl(0, F_GETFL) | O_NONBLOCK);
  out = dup(1);
  if (out < 0 || dup2(2, 1) < 0) out = 1;
  else fcntl(out, F_SETFD, FD_CLOEXEC);

  void *lib = dlopen(core_path, RTLD_NOW | RTLD_LOCAL);
  if (!lib) fail("couldn't load the emulator: %s", dlerror());
  core.init = sym(lib, "retro_init");
  core.deinit = sym(lib, "retro_deinit");
  core.get_system_info = sym(lib, "retro_get_system_info");
  core.get_system_av_info = sym(lib, "retro_get_system_av_info");
  core.set_environment = sym(lib, "retro_set_environment");
  core.set_video_refresh = sym(lib, "retro_set_video_refresh");
  core.set_audio_sample = sym(lib, "retro_set_audio_sample");
  core.set_audio_sample_batch = sym(lib, "retro_set_audio_sample_batch");
  core.set_input_poll = sym(lib, "retro_set_input_poll");
  core.set_input_state = sym(lib, "retro_set_input_state");
  core.set_controller_port_device = sym(lib, "retro_set_controller_port_device");
  core.load_game = sym(lib, "retro_load_game");
  core.unload_game = sym(lib, "retro_unload_game");
  core.run = sym(lib, "retro_run");
  core.get_memory_data = sym(lib, "retro_get_memory_data");
  core.get_memory_size = sym(lib, "retro_get_memory_size");

  read_option_overrides();
  core.set_environment(environment);
  core.init();
  core.set_video_refresh(video);
  core.set_audio_sample(audio_sample);
  core.set_audio_sample_batch(audio_batch);
  core.set_input_poll(input_poll);
  core.set_input_state(input_state);

  struct retro_system_info sys = {0};
  core.get_system_info(&sys);
  struct retro_game_info info = {0};
  if (strcmp(game, "-") != 0) {
    info.path = game;
    if (!sys.need_fullpath) {
      size_t len = 0;
      void *data = read_file(game, &len);
      if (!data) fail("couldn't read %s: %s", game, strerror(errno));
      info.data = data;
      info.size = len;
    }
    const char *base = strrchr(game, '/');
    base = base ? base + 1 : game;
    snprintf(save_path, sizeof save_path, "%s/%s.srm", save_dir, base);
  }
  if (!core.load_game(strcmp(game, "-") != 0 ? &info : NULL)) fail("the emulator couldn't start this game");
  for (unsigned p = 0; p < MAX_PORTS; p++) core.set_controller_port_device(p, RETRO_DEVICE_JOYPAD);
  read_save();

  struct retro_system_av_info av = {0};
  core.get_system_av_info(&av);
  out_w = av.geometry.base_width;
  out_h = av.geometry.base_height;
  double fps = av.timing.fps > 1 ? av.timing.fps : 60;
  if (!out_w || !out_h || out_w > 4096 || out_h > 4096) fail("the emulator gave a picture size of %ux%u", out_w, out_h);
  frame = calloc((size_t)out_w * out_h, 4);
  if (!frame) fail("out of memory");
  double aspect = av.geometry.aspect_ratio > 0 ? av.geometry.aspect_ratio : (double)out_w / out_h;
  char hello[256];
  int n = snprintf(hello, sizeof hello, "{\"width\":%u,\"height\":%u,\"fps\":%.6f,\"sampleRate\":%.3f,\"aspect\":%.6f}",
                   out_w, out_h, fps, av.timing.sample_rate, aspect);
  send_msg('I', hello, (uint32_t)n);

  // Play in real time: one frame every 1/fps seconds, by the clock. Falling
  // more than a few frames behind (the server was busy) starts the count
  // again instead of rushing to catch up.
  double step = 1.0 / fps, next = now(), last_save_at = next;
  for (;;) {
    if (!read_input()) break;
    sound_len = 0;
    core.run();
    send_msg('V', frame, out_w * out_h * 4);
    if (sound_len) send_msg('A', sound, (uint32_t)(sound_len * sizeof *sound));
    next += step;
    double t = now();
    if (t - next > 5 * step) next = t;
    else if (next > t) sleep_until(next);
    if (t - last_save_at > 30) {
      write_save();
      last_save_at = t;
    }
  }
  write_save();
  core.unload_game();
  core.deinit();
  return 0;
}
