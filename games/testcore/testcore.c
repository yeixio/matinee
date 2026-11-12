// SPDX-License-Identifier: MIT
//
// The lag test: a tiny libretro "game" that needs no game file. A square
// moves with the d-pad, the whole picture flashes white while A or B is held
// (film the screen and the controller together to time it), and Start
// plays a beep. Matinee's tests also use it to check that pictures, sound
// and button presses make it through.

#include <stdbool.h>
#include <stdint.h>
#include <string.h>

#include "../runner/libretro.h"

#define W 320
#define H 240
#define RATE 48000
#define FPS 60

static uint32_t fb[W * H];
static int16_t snd[RATE / FPS * 2];
static int x = W / 2 - 16, y = H / 2 - 16;
static unsigned tick;
static double phase;

static retro_environment_t env;
static retro_video_refresh_t video;
static retro_audio_sample_batch_t audio;
static retro_input_poll_t poll_input;
static retro_input_state_t input;

RETRO_API void retro_set_environment(retro_environment_t cb) {
  env = cb;
  bool yes = true;
  cb(RETRO_ENVIRONMENT_SET_SUPPORT_NO_GAME, &yes);
}
RETRO_API void retro_set_video_refresh(retro_video_refresh_t cb) { video = cb; }
RETRO_API void retro_set_audio_sample(retro_audio_sample_t cb) { (void)cb; }
RETRO_API void retro_set_audio_sample_batch(retro_audio_sample_batch_t cb) { audio = cb; }
RETRO_API void retro_set_input_poll(retro_input_poll_t cb) { poll_input = cb; }
RETRO_API void retro_set_input_state(retro_input_state_t cb) { input = cb; }
RETRO_API void retro_init(void) {}
RETRO_API void retro_deinit(void) {}
RETRO_API unsigned retro_api_version(void) { return RETRO_API_VERSION; }

RETRO_API void retro_get_system_info(struct retro_system_info *info) {
  memset(info, 0, sizeof *info);
  info->library_name = "Matinee lag test";
  info->library_version = "1";
  info->valid_extensions = "";
}

RETRO_API void retro_get_system_av_info(struct retro_system_av_info *info) {
  memset(info, 0, sizeof *info);
  info->geometry.base_width = info->geometry.max_width = W;
  info->geometry.base_height = info->geometry.max_height = H;
  info->geometry.aspect_ratio = 4.0f / 3.0f;
  info->timing.fps = FPS;
  info->timing.sample_rate = RATE;
}

RETRO_API void retro_set_controller_port_device(unsigned port, unsigned device) {
  (void)port;
  (void)device;
}
RETRO_API void retro_reset(void) {}
RETRO_API size_t retro_serialize_size(void) { return 0; }
RETRO_API bool retro_serialize(void *data, size_t size) {
  (void)data;
  (void)size;
  return false;
}
RETRO_API bool retro_unserialize(const void *data, size_t size) {
  (void)data;
  (void)size;
  return false;
}
RETRO_API void retro_cheat_reset(void) {}
RETRO_API void retro_cheat_set(unsigned index, bool enabled, const char *code) {
  (void)index;
  (void)enabled;
  (void)code;
}
RETRO_API bool retro_load_game(const struct retro_game_info *game) {
  (void)game;
  enum retro_pixel_format f = RETRO_PIXEL_FORMAT_XRGB8888;
  return env(RETRO_ENVIRONMENT_SET_PIXEL_FORMAT, &f);
}
RETRO_API bool retro_load_game_special(unsigned type, const struct retro_game_info *info, size_t num) {
  (void)type;
  (void)info;
  (void)num;
  return false;
}
RETRO_API void retro_unload_game(void) {}
RETRO_API unsigned retro_get_region(void) { return RETRO_REGION_NTSC; }
RETRO_API void *retro_get_memory_data(unsigned id) {
  (void)id;
  return NULL;
}
RETRO_API size_t retro_get_memory_size(unsigned id) {
  (void)id;
  return 0;
}

static bool held(unsigned id) { return input(0, RETRO_DEVICE_JOYPAD, 0, id) != 0; }

RETRO_API void retro_run(void) {
  poll_input();
  if (held(RETRO_DEVICE_ID_JOYPAD_LEFT) && x > 0) x -= 4;
  if (held(RETRO_DEVICE_ID_JOYPAD_RIGHT) && x < W - 32) x += 4;
  if (held(RETRO_DEVICE_ID_JOYPAD_UP) && y > 0) y -= 4;
  if (held(RETRO_DEVICE_ID_JOYPAD_DOWN) && y < H - 32) y += 4;
  bool flash = held(RETRO_DEVICE_ID_JOYPAD_A) || held(RETRO_DEVICE_ID_JOYPAD_B);
  uint32_t bg = flash ? 0xffffff : 0x1a1420;
  for (int i = 0; i < W * H; i++) fb[i] = bg;
  // A bar that sweeps once a second, to show the picture is moving.
  int bar = (int)(tick % FPS) * W / FPS;
  for (int j = H - 8; j < H; j++)
    for (int i = 0; i < 8; i++) fb[j * W + (bar + i) % W] = 0x888888;
  for (int j = y; j < y + 32; j++)
    for (int i = x; i < x + 32; i++) fb[j * W + i] = 0xff6f5e;
  video(fb, W, H, W * 4);

  bool beep = held(RETRO_DEVICE_ID_JOYPAD_START);
  for (int i = 0; i < RATE / FPS; i++) {
    int16_t s = 0;
    if (beep) {
      phase += 440.0 / RATE;
      if (phase >= 1) phase -= 1;
      s = phase < 0.5 ? 6000 : -6000;
    }
    snd[2 * i] = snd[2 * i + 1] = s;
  }
  audio(snd, RATE / FPS);
  tick++;
}
