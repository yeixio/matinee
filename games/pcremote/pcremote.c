// SPDX-License-Identifier: GPL-3.0-or-later
//
// matinee-pcremote plays a gaming PC on the home network through Matinee:
// it connects to Sunshine (https://github.com/LizardByte/Sunshine), the
// free game streaming host, receives the picture and sound of the game the
// PC runs (Steam's Big Picture, usually) and hands them to Matinee, which
// streams them to a player like any other game. It's built on
// moonlight-common-c (https://github.com/moonlight-stream/moonlight-common-c,
// GPL-3.0) and runs as its own process, as the game runner does.
//
// Matinee itself pairs with the PC and asks it to start the game, over
// Sunshine's HTTPS API (internal/pcremote); this program only streams.
//
//   matinee-pcremote query
//   matinee-pcremote play <host> <width> <height> <fps> <bitrate kbps>
//
// query prints {"launchQuery":…}: what Matinee adds to its launch request,
// so the PC speaks the protocol this program does.
//
// play's details come in the environment: MATINEE_PC_APPVERSION and
// MATINEE_PC_GFEVERSION (from the PC's serverinfo), MATINEE_PC_RTSP (the
// session URL the launch gave), MATINEE_PC_CODECS (ServerCodecModeSupport),
// MATINEE_PC_RIKEY (32 hex digits) and MATINEE_PC_RIKEYID (a number): the
// input key sent with the launch.
//
// Its output (stdout) is messages, each an 8-byte header (a type letter, 3
// bytes of padding, the payload's length as a little-endian uint32) and the
// payload, as the PlayStation bridge's are:
//
//   'I'  JSON {"width":W,"height":H,"fps":F}, once connected.
//   'V'  one H.264 access unit, Annex B.
//   'A'  one Opus packet, 48 kHz stereo.
//   'E'  JSON {"error":…}: why the stream ended. Then it exits.
//
// Its input (stdin) has the same headers, all numbers little-endian:
//
//   'C'  a controller: number (uint8), the mask of controllers present
//        (uint16), buttons (uint32, Moonlight's flags), the triggers
//        (uint8 each), the sticks lx ly rx ry (int16 each).
//   'k'  a key: Windows virtual-key code (uint16), down (uint8),
//        modifiers (uint8, Moonlight's).
//   'm'  the mouse moved: dx dy (int16 each).
//   'p'  the pointer to a spot: x y (int16 each) on a picture of width
//        and height (int16 each).
//   'b'  a mouse button: Moonlight's number (uint8), down (uint8).
//   'w'  the wheel: dy dx (int8 each, in notches).
//   'K'  asks the PC for a keyframe, for a player that just joined.
//   'Q'  stops. Closing stdin does too.
//
// Logs go to stderr.

#include <Limelight.h>

#include <pthread.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static pthread_mutex_t out_mu = PTHREAD_MUTEX_INITIALIZER;
static pthread_mutex_t done_mu = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t done_cond = PTHREAD_COND_INITIALIZER;
static int done;
static int sent_error;
static STREAM_CONFIGURATION config;

// json_str prints s as a JSON string.
static void json_str(FILE *f, const char *s)
{
	fputc('"', f);
	for(; s && *s; s++)
	{
		unsigned char c = (unsigned char)*s;
		if(c == '"' || c == '\\')
			fprintf(f, "\\%c", c);
		else if(c < 0x20)
			fprintf(f, "\\u%04x", c);
		else
			fputc(c, f);
	}
	fputc('"', f);
}

static void put_msg(char type, const void *b, size_t n)
{
	uint8_t h[8] = { (uint8_t)type, 0, 0, 0, n & 0xff, (n >> 8) & 0xff, (n >> 16) & 0xff, (n >> 24) & 0xff };
	pthread_mutex_lock(&out_mu);
	fwrite(h, 1, 8, stdout);
	if(n)
		fwrite(b, 1, n, stdout);
	fflush(stdout);
	pthread_mutex_unlock(&out_mu);
}

static void finish(void)
{
	pthread_mutex_lock(&done_mu);
	done = 1;
	pthread_cond_signal(&done_cond);
	pthread_mutex_unlock(&done_mu);
}

// put_error sends why the stream ended, once.
static void put_error(const char *fmt, ...)
{
	char msg[400], buf[600];
	va_list ap;
	va_start(ap, fmt);
	vsnprintf(msg, sizeof(msg), fmt, ap);
	va_end(ap);
	pthread_mutex_lock(&done_mu);
	int first = !sent_error;
	sent_error = 1;
	pthread_mutex_unlock(&done_mu);
	if(!first)
		return;
	FILE *f = fmemopen(buf, sizeof(buf), "w");
	fputs("{\"error\":", f);
	json_str(f, msg);
	fputs("}", f);
	long n = ftell(f);
	fclose(f);
	put_msg('E', buf, (size_t)n);
}

// ---- connection ----

static void log_cb(const char *format, ...)
{
	va_list ap;
	va_start(ap, format);
	vfprintf(stderr, format, ap);
	va_end(ap);
}

static void stage_failed(int stage, int code)
{
	put_error("couldn't connect to the PC (%s, error %d): check that Sunshine's ports aren't blocked by a firewall",
		LiGetStageName(stage), code);
}

static void connection_started(void)
{
	char buf[128];
	int n = snprintf(buf, sizeof(buf), "{\"width\":%d,\"height\":%d,\"fps\":%d}", config.width, config.height, config.fps);
	put_msg('I', buf, (size_t)n);
}

static void connection_terminated(int code)
{
	switch(code)
	{
		case ML_ERROR_GRACEFUL_TERMINATION:
			put_error("the PC ended the stream");
			break;
		case ML_ERROR_NO_VIDEO_TRAFFIC:
			put_error("no picture came from the PC: check that its firewall lets Sunshine's UDP ports through");
			break;
		case ML_ERROR_PROTECTED_CONTENT:
			put_error("the PC is showing something that can't be streamed");
			break;
		default:
			put_error("the connection to the PC was lost (error %d)", code);
	}
	finish();
}

// ---- picture and sound ----

static uint8_t *frame;
static size_t frame_cap;

static int video_setup(int format, int width, int height, int fps, void *ctx, int flags)
{
	(void)width; (void)height; (void)fps; (void)ctx; (void)flags;
	if(!(format & VIDEO_FORMAT_MASK_H264))
	{
		put_error("the PC can't send H.264");
		return -1;
	}
	return 0;
}

static int video_submit(PDECODE_UNIT du)
{
	if((size_t)du->fullLength > frame_cap)
	{
		size_t cap = (size_t)du->fullLength * 2;
		uint8_t *b = realloc(frame, cap);
		if(!b)
			return DR_NEED_IDR;
		frame = b;
		frame_cap = cap;
	}
	size_t n = 0;
	for(PLENTRY e = du->bufferList; e; e = e->next)
	{
		if(n + (size_t)e->length > frame_cap)
			return DR_NEED_IDR;
		memcpy(frame + n, e->data, (size_t)e->length);
		n += (size_t)e->length;
	}
	put_msg('V', frame, n);
	return DR_OK;
}

static int audio_init(int audio_config, const POPUS_MULTISTREAM_CONFIGURATION opus, void *ctx, int flags)
{
	(void)audio_config; (void)ctx; (void)flags;
	// Stereo comes as one Opus stream of two coupled channels: plain Opus
	// packets, which go to the player as they are.
	if(opus->channelCount != 2 || opus->streams != 1 || opus->coupledStreams != 1)
	{
		put_error("the PC sent %d-channel sound", opus->channelCount);
		return -1;
	}
	fprintf(stderr, "I audio: %d Hz, frames of %d\n", opus->sampleRate, opus->samplesPerFrame);
	return 0;
}

static void audio_sample(char *data, int n)
{
	if(n > 0)
		put_msg('A', data, (size_t)n);
}

// ---- input ----

static int read_full(void *b, size_t n)
{
	uint8_t *p = b;
	while(n > 0)
	{
		ssize_t r = read(0, p, n);
		if(r <= 0)
			return -1;
		p += r;
		n -= (size_t)r;
	}
	return 0;
}

static int16_t le16(const uint8_t *b) { return (int16_t)(b[0] | b[1] << 8); }

static void read_input(void)
{
	uint8_t h[8], b[64];
	for(;;)
	{
		if(read_full(h, 8) != 0)
			return;
		uint32_t n = h[4] | h[5] << 8 | h[6] << 16 | (uint32_t)h[7] << 24;
		if(n > sizeof(b))
			return;
		if(n && read_full(b, n) != 0)
			return;
		switch(h[0])
		{
			case 'C':
				if(n < 17)
					break;
				LiSendMultiControllerEvent(b[0], (uint16_t)le16(b + 1),
					(int)(b[3] | b[4] << 8 | b[5] << 16 | (uint32_t)b[6] << 24), b[7], b[8],
					le16(b + 9), le16(b + 11), le16(b + 13), le16(b + 15));
				break;
			case 'k':
				if(n < 4)
					break;
				LiSendKeyboardEvent(le16(b), b[2] ? KEY_ACTION_DOWN : KEY_ACTION_UP, (char)b[3]);
				break;
			case 'm':
				if(n < 4)
					break;
				LiSendMouseMoveEvent(le16(b), le16(b + 2));
				break;
			case 'p':
				if(n < 8)
					break;
				LiSendMousePositionEvent(le16(b), le16(b + 2), le16(b + 4), le16(b + 6));
				break;
			case 'b':
				if(n < 2)
					break;
				LiSendMouseButtonEvent(b[1] ? BUTTON_ACTION_PRESS : BUTTON_ACTION_RELEASE, b[0]);
				break;
			case 'w':
				if(n < 2)
					break;
				if(b[0])
					LiSendScrollEvent((signed char)b[0]);
				if(b[1])
					LiSendHScrollEvent((signed char)b[1]);
				break;
			case 'K':
				LiRequestIdrFrame();
				break;
			case 'Q':
				return;
		}
	}
}

static void *input_thread(void *user)
{
	(void)user;
	read_input();
	finish();
	return NULL;
}

// ---- play ----

static int hex_decode(const char *s, char *out, size_t n)
{
	if(!s || strlen(s) != n * 2)
		return -1;
	for(size_t i = 0; i < n; i++)
	{
		unsigned v;
		if(sscanf(s + 2 * i, "%2x", &v) != 1)
			return -1;
		out[i] = (char)v;
	}
	return 0;
}

static int cmd_play(const char *host, int width, int height, int fps, int bitrate)
{
	SERVER_INFORMATION server;
	LiInitializeServerInformation(&server);
	server.address = host;
	server.serverInfoAppVersion = getenv("MATINEE_PC_APPVERSION");
	server.serverInfoGfeVersion = getenv("MATINEE_PC_GFEVERSION");
	server.rtspSessionUrl = getenv("MATINEE_PC_RTSP");
	const char *codecs = getenv("MATINEE_PC_CODECS");
	server.serverCodecModeSupport = codecs ? atoi(codecs) : SCM_H264;
	if(!server.serverInfoAppVersion)
	{
		put_error("the PC's version is missing");
		return 1;
	}

	LiInitializeStreamConfiguration(&config);
	config.width = width;
	config.height = height;
	config.fps = fps;
	config.bitrate = bitrate;
	config.packetSize = 1392;
	config.streamingRemotely = STREAM_CFG_LOCAL;
	config.audioConfiguration = AUDIO_CONFIGURATION_STEREO;
	// H.264, which every player decodes.
	config.supportedVideoFormats = VIDEO_FORMAT_H264;
	config.colorSpace = COLORSPACE_REC_709;
	config.colorRange = COLOR_RANGE_LIMITED;
	// The sound is encrypted (cheap); the picture isn't, on the home
	// network, so a small server keeps up.
	config.encryptionFlags = ENCFLG_AUDIO;
	if(hex_decode(getenv("MATINEE_PC_RIKEY"), config.remoteInputAesKey, sizeof(config.remoteInputAesKey)) != 0)
	{
		put_error("the input key is missing");
		return 1;
	}
	const char *keyid = getenv("MATINEE_PC_RIKEYID");
	uint32_t id = keyid ? (uint32_t)strtoul(keyid, NULL, 10) : 0;
	memset(config.remoteInputAesIv, 0, sizeof(config.remoteInputAesIv));
	config.remoteInputAesIv[0] = (char)(id >> 24);
	config.remoteInputAesIv[1] = (char)(id >> 16);
	config.remoteInputAesIv[2] = (char)(id >> 8);
	config.remoteInputAesIv[3] = (char)id;

	CONNECTION_LISTENER_CALLBACKS conn;
	LiInitializeConnectionCallbacks(&conn);
	conn.stageFailed = stage_failed;
	conn.connectionStarted = connection_started;
	conn.connectionTerminated = connection_terminated;
	conn.logMessage = log_cb;

	DECODER_RENDERER_CALLBACKS video;
	LiInitializeVideoCallbacks(&video);
	video.setup = video_setup;
	video.submitDecodeUnit = video_submit;

	AUDIO_RENDERER_CALLBACKS audio;
	LiInitializeAudioCallbacks(&audio);
	audio.init = audio_init;
	audio.decodeAndPlaySample = audio_sample;

	pthread_t in;
	pthread_create(&in, NULL, input_thread, NULL);
	pthread_detach(in);

	if(LiStartConnection(&server, &config, &conn, &video, &audio, NULL, 0, NULL, 0) != 0)
	{
		put_error("couldn't connect to the PC");
		return 1;
	}
	pthread_mutex_lock(&done_mu);
	while(!done)
		pthread_cond_wait(&done_cond, &done_mu);
	pthread_mutex_unlock(&done_mu);
	LiStopConnection();
	put_error("the stream ended");
	return 0;
}

int main(int argc, char *argv[])
{
	if(argc >= 2 && strcmp(argv[1], "query") == 0)
	{
		fputs("{\"launchQuery\":", stdout);
		json_str(stdout, LiGetLaunchUrlQueryParameters());
		fputs("}\n", stdout);
		return 0;
	}
	if(argc >= 7 && strcmp(argv[1], "play") == 0)
		return cmd_play(argv[2], atoi(argv[3]), atoi(argv[4]), atoi(argv[5]), atoi(argv[6]));
	fprintf(stderr, "usage: matinee-pcremote query | play <host> <width> <height> <fps> <bitrate kbps>\n");
	return 2;
}
