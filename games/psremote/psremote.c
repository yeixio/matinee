// SPDX-License-Identifier: AGPL-3.0-only
//
// matinee-psremote connects to a PlayStation 4 or 5 on the home network
// with Remote Play and hands its picture and sound to Matinee, which
// streams them to a player like any other game. It's built on chiaki-ng's
// library (https://github.com/streetpea/chiaki-ng, AGPL-3.0 with an
// OpenSSL exception) and runs as its own process, as the game runner does.
// Not made or endorsed by Sony.
//
//   matinee-psremote status <host>
//   matinee-psremote regist <host> ps4|ps5
//   matinee-psremote wake   <host> ps4|ps5
//   matinee-psremote play   <host> ps4|ps5 <height> <fps>
//
// The secrets come in the environment, to stay out of process listings:
// MATINEE_PS_ACCOUNT (the PSN account ID, 16 hex digits), MATINEE_PS_PIN
// (the 8-digit code the console shows when pairing), MATINEE_PS_REGIST_KEY
// and MATINEE_PS_RP_KEY (what pairing gave, 32 hex digits each).
//
// status prints {"state":"ready"|"standby","name":…,"ps5":…,"app":…}
// and exits 0, or exits 1 if the console didn't answer. regist pairs and
// prints {"name":…,"mac":…,"registKey":…,"rpKey":…,"target":…}. wake
// sends the packet that wakes the console from rest mode. On failure they
// print {"error":…} and exit 1.
//
// play streams. Its output (stdout) is messages, each an 8-byte header (a
// type letter, 3 bytes of padding, the payload's length as a little-endian
// uint32) and the payload, as the game runner's are:
//
//   'I'  JSON {"width":W,"height":H,"fps":F}, once connected.
//   'V'  one H.264 access unit, Annex B.
//   'A'  one Opus packet, 48 kHz stereo.
//   'E'  JSON {"error":…}: why the stream ended. Then it exits.
//
// Its input (stdin) has the same headers:
//
//   'C'  the controller: buttons (uint32, chiaki's bits), L2 and R2
//        (uint8 each), the sticks lx ly rx ry (int16 each), little-endian.
//   'K'  asks the console for a keyframe, for a player that just joined.
//   'Q'  stops. Closing stdin does too.
//
// Logs go to stderr.

#include <chiaki/common.h>
#include <chiaki/controller.h>
#include <chiaki/discovery.h>
#include <chiaki/log.h>
#include <chiaki/regist.h>
#include <chiaki/session.h>

#include <netdb.h>
#include <netinet/in.h>
#include <pthread.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static ChiakiLog logger;
static pthread_mutex_t out_mu = PTHREAD_MUTEX_INITIALIZER;

static void log_cb(ChiakiLogLevel level, const char *msg, void *user)
{
	(void)user;
	if(level == CHIAKI_LOG_VERBOSE)
		return;
	fprintf(stderr, "%c %s\n", chiaki_log_level_char(level), msg);
}

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

static int fail(const char *fmt, ...)
{
	char buf[512];
	va_list ap;
	va_start(ap, fmt);
	vsnprintf(buf, sizeof(buf), fmt, ap);
	va_end(ap);
	fputs("{\"error\":", stdout);
	json_str(stdout, buf);
	fputs("}\n", stdout);
	fflush(stdout);
	return 1;
}

static int hex_decode(const char *s, uint8_t *out, size_t n)
{
	if(!s || strlen(s) != n * 2)
		return -1;
	for(size_t i = 0; i < n; i++)
	{
		unsigned v;
		if(sscanf(s + 2 * i, "%2x", &v) != 1)
			return -1;
		out[i] = (uint8_t)v;
	}
	return 0;
}

static void hex_print(const uint8_t *b, size_t n)
{
	fputc('"', stdout);
	for(size_t i = 0; i < n; i++)
		printf("%02x", b[i]);
	fputc('"', stdout);
}

// ---- status ----

typedef struct
{
	bool found;
	ChiakiDiscoveryHostState state;
	bool ps5;
	ChiakiTarget target;
	char name[128], app[128];
} Found;

static void discovery_cb(ChiakiDiscoveryHost *host, void *user)
{
	Found *f = user;
	f->found = true;
	f->state = host->state;
	f->ps5 = chiaki_discovery_host_is_ps5(host);
	f->target = chiaki_discovery_host_system_version_target(host);
	snprintf(f->name, sizeof(f->name), "%s", host->host_name ? host->host_name : "");
	snprintf(f->app, sizeof(f->app), "%s", host->running_app_name ? host->running_app_name : "");
}

// discover asks the console at host what it is and whether it's awake.
static int discover(const char *host, Found *f)
{
	memset(f, 0, sizeof(*f));
	struct addrinfo hints = { 0 }, *ais;
	hints.ai_socktype = SOCK_DGRAM;
	hints.ai_family = strchr(host, ':') ? AF_INET6 : AF_INET;
	if(getaddrinfo(host, NULL, &hints, &ais) != 0)
		return -1;
	struct sockaddr_storage addr;
	socklen_t addr_len = 0;
	for(struct addrinfo *ai = ais; ai; ai = ai->ai_next)
	{
		if(ai->ai_family == AF_INET || ai->ai_family == AF_INET6)
		{
			memcpy(&addr, ai->ai_addr, ai->ai_addrlen);
			addr_len = ai->ai_addrlen;
			break;
		}
	}
	freeaddrinfo(ais);
	if(!addr_len)
		return -1;

	ChiakiDiscovery discovery;
	if(chiaki_discovery_init(&discovery, &logger, addr.ss_family) != CHIAKI_ERR_SUCCESS)
		return -1;
	ChiakiDiscoveryThread thread;
	if(chiaki_discovery_thread_start_oneshot(&thread, &discovery, discovery_cb, f) != CHIAKI_ERR_SUCCESS)
	{
		chiaki_discovery_fini(&discovery);
		return -1;
	}
	ChiakiDiscoveryPacket packet = { 0 };
	packet.cmd = CHIAKI_DISCOVERY_CMD_SRCH;
	// Asked as a PS4 and as a PS5; whichever it is answers.
	const struct { const char *version; uint16_t port; } asks[] = {
		{ CHIAKI_DISCOVERY_PROTOCOL_VERSION_PS4, CHIAKI_DISCOVERY_PORT_PS4 },
		{ CHIAKI_DISCOVERY_PROTOCOL_VERSION_PS5, CHIAKI_DISCOVERY_PORT_PS5 },
	};
	for(size_t i = 0; i < 2; i++)
	{
		packet.protocol_version = (char *)asks[i].version;
		if(addr.ss_family == AF_INET)
			((struct sockaddr_in *)&addr)->sin_port = htons(asks[i].port);
		else
			((struct sockaddr_in6 *)&addr)->sin6_port = htons(asks[i].port);
		chiaki_discovery_send(&discovery, &packet, (struct sockaddr *)&addr, addr_len);
	}
	if(chiaki_thread_timedjoin(&thread.thread, NULL, 2000) != CHIAKI_ERR_SUCCESS)
		chiaki_discovery_thread_stop(&thread);
	chiaki_discovery_fini(&discovery);
	return f->found ? 0 : -1;
}

static int cmd_status(const char *host)
{
	Found f;
	if(discover(host, &f) != 0)
		return fail("no PlayStation answered at %s", host);
	printf("{\"state\":\"%s\",\"ps5\":%s,\"name\":", f.state == CHIAKI_DISCOVERY_HOST_STATE_READY ? "ready" : "standby",
		f.ps5 ? "true" : "false");
	json_str(stdout, f.name);
	fputs(",\"app\":", stdout);
	json_str(stdout, f.app);
	fputs("}\n", stdout);
	return 0;
}

// ---- regist ----

typedef struct
{
	ChiakiCond cond;
	ChiakiMutex mu;
	bool done;
	bool ok;
	ChiakiRegisteredHost host;
} RegistResult;

static void regist_cb(ChiakiRegistEvent *event, void *user)
{
	RegistResult *r = user;
	chiaki_mutex_lock(&r->mu);
	r->done = true;
	r->ok = event->type == CHIAKI_REGIST_EVENT_TYPE_FINISHED_SUCCESS;
	if(r->ok)
		r->host = *event->registered_host;
	chiaki_mutex_unlock(&r->mu);
	chiaki_cond_signal(&r->cond);
}

static bool regist_done(void *user) { return ((RegistResult *)user)->done; }

static int cmd_regist(const char *host, bool ps5)
{
	ChiakiRegistInfo info = { 0 };
	info.host = host;
	if(hex_decode(getenv("MATINEE_PS_ACCOUNT"), info.psn_account_id, sizeof(info.psn_account_id)) != 0)
		return fail("the PSN account ID is missing");
	const char *pin = getenv("MATINEE_PS_PIN");
	if(!pin || strlen(pin) != 8 || strspn(pin, "0123456789") != 8)
		return fail("the pairing code is 8 digits");
	info.pin = (uint32_t)strtoul(pin, NULL, 10);
	info.target = ps5 ? CHIAKI_TARGET_PS5_1 : CHIAKI_TARGET_PS4_10;
	Found f;
	if(discover(host, &f) == 0 && !chiaki_target_is_unknown(f.target))
		info.target = f.target;

	RegistResult r = { 0 };
	chiaki_cond_init(&r.cond);
	chiaki_mutex_init(&r.mu, false);
	ChiakiRegist regist;
	if(chiaki_regist_start(&regist, &logger, &info, regist_cb, &r) != CHIAKI_ERR_SUCCESS)
		return fail("couldn't start pairing");
	chiaki_mutex_lock(&r.mu);
	chiaki_cond_timedwait_pred(&r.cond, &r.mu, 30000, regist_done, &r);
	bool done = r.done, ok = r.ok;
	chiaki_mutex_unlock(&r.mu);
	if(!done)
		chiaki_regist_stop(&regist);
	chiaki_regist_fini(&regist);
	if(!ok)
		return fail("the console didn't accept the pairing: check the code and the account");

	char name[sizeof(r.host.server_nickname) + 1] = { 0 };
	memcpy(name, r.host.server_nickname, sizeof(r.host.server_nickname));
	fputs("{\"name\":", stdout);
	json_str(stdout, name);
	printf(",\"mac\":\"%02x:%02x:%02x:%02x:%02x:%02x\",\"target\":%d,\"registKey\":",
		r.host.server_mac[0], r.host.server_mac[1], r.host.server_mac[2],
		r.host.server_mac[3], r.host.server_mac[4], r.host.server_mac[5], (int)r.host.target);
	hex_print((const uint8_t *)r.host.rp_regist_key, sizeof(r.host.rp_regist_key));
	fputs(",\"rpKey\":", stdout);
	hex_print(r.host.rp_key, sizeof(r.host.rp_key));
	fputs("}\n", stdout);
	return 0;
}

// ---- wake ----

static int read_regist_key(char key[CHIAKI_SESSION_AUTH_SIZE])
{
	return hex_decode(getenv("MATINEE_PS_REGIST_KEY"), (uint8_t *)key, CHIAKI_SESSION_AUTH_SIZE);
}

static int cmd_wake(const char *host, bool ps5)
{
	char key[CHIAKI_SESSION_AUTH_SIZE + 1] = { 0 };
	if(read_regist_key(key) != 0)
		return fail("the console isn't paired");
	// The wake-up credential is the regist key's text read as hex.
	uint64_t credential = (uint64_t)strtoull(key, NULL, 16);
	if(chiaki_discovery_wakeup(&logger, NULL, host, credential, ps5) != CHIAKI_ERR_SUCCESS)
		return fail("couldn't send the wake-up");
	puts("{}");
	return 0;
}

// ---- play ----

static ChiakiSession session;
static ChiakiConnectVideoProfile profile;

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

static void put_error(const char *msg)
{
	char buf[600];
	FILE *f = fmemopen(buf, sizeof(buf), "w");
	fputs("{\"error\":", f);
	json_str(f, msg);
	fputs("}", f);
	long n = ftell(f);
	fclose(f);
	put_msg('E', buf, (size_t)n);
}

static void event_cb(ChiakiEvent *event, void *user)
{
	(void)user;
	switch(event->type)
	{
		case CHIAKI_EVENT_CONNECTED:
		{
			char buf[128];
			int n = snprintf(buf, sizeof(buf), "{\"width\":%u,\"height\":%u,\"fps\":%u}",
				session.connect_info.video_profile.width, session.connect_info.video_profile.height,
				session.connect_info.video_profile.max_fps);
			put_msg('I', buf, (size_t)n);
			break;
		}
		case CHIAKI_EVENT_LOGIN_PIN_REQUEST:
			put_error("the console asks for a login passcode: turn it off for this user on the PlayStation");
			chiaki_session_stop(&session);
			break;
		case CHIAKI_EVENT_QUIT:
			if(event->quit.reason_str)
				put_error(event->quit.reason_str);
			else
				put_error(chiaki_quit_reason_string(event->quit.reason));
			break;
		default:
			break;
	}
}

static bool video_cb(uint8_t *buf, size_t size, int32_t frames_lost, bool frame_recovered, void *user)
{
	(void)user;
	// A frame lost and not recovered leaves the picture broken until the
	// next keyframe: ask for one now.
	if(frames_lost > 0 && !frame_recovered)
		chiaki_session_request_idr(&session);
	put_msg('V', buf, size);
	return true;
}

static void audio_header_cb(ChiakiAudioHeader *header, void *user)
{
	(void)user;
	fprintf(stderr, "I audio: %u channels, %u Hz, frames of %u\n", header->channels, header->rate, header->frame_size);
}

static void audio_frame_cb(uint8_t *buf, size_t size, void *user)
{
	(void)user;
	put_msg('A', buf, size);
}

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
	ChiakiControllerState st;
	chiaki_controller_state_set_idle(&st);
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
				if(n < 14)
					break;
				st.buttons = b[0] | b[1] << 8 | b[2] << 16 | (uint32_t)b[3] << 24;
				st.l2_state = b[4];
				st.r2_state = b[5];
				st.left_x = le16(b + 6);
				st.left_y = le16(b + 8);
				st.right_x = le16(b + 10);
				st.right_y = le16(b + 12);
				chiaki_session_set_controller_state(&session, &st);
				break;
			case 'K':
				chiaki_session_request_idr(&session);
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
	chiaki_session_stop(&session);
	return NULL;
}

static int cmd_play(const char *host, bool ps5, int height, int fps)
{
	ChiakiConnectInfo info;
	memset(&info, 0, sizeof(info));
	info.ps5 = ps5;
	info.host = host;
	if(read_regist_key(info.regist_key) != 0 || hex_decode(getenv("MATINEE_PS_RP_KEY"), info.morning, sizeof(info.morning)) != 0)
	{
		put_error("the console isn't paired");
		return 1;
	}
	if(hex_decode(getenv("MATINEE_PS_ACCOUNT"), info.psn_account_id, sizeof(info.psn_account_id)) != 0)
	{
		put_error("the PSN account ID is missing");
		return 1;
	}
	ChiakiVideoResolutionPreset res = CHIAKI_VIDEO_RESOLUTION_PRESET_1080p;
	if(height <= 540)
		res = CHIAKI_VIDEO_RESOLUTION_PRESET_540p;
	else if(height <= 720)
		res = CHIAKI_VIDEO_RESOLUTION_PRESET_720p;
	chiaki_connect_video_profile_preset(&profile, res, fps <= 30 ? CHIAKI_VIDEO_FPS_PRESET_30 : CHIAKI_VIDEO_FPS_PRESET_60);
	// H.264, which every player decodes.
	profile.codec = CHIAKI_CODEC_H264;
	info.video_profile = profile;
	info.video_profile_auto_downgrade = true;
	info.packet_loss_max = 0.05;
	info.enable_idr_on_fec_failure = true;

	if(chiaki_session_init(&session, &info, &logger) != CHIAKI_ERR_SUCCESS)
	{
		put_error("couldn't start Remote Play");
		return 1;
	}
	chiaki_session_set_event_cb(&session, event_cb, NULL);
	chiaki_session_set_video_sample_cb(&session, video_cb, NULL);
	ChiakiAudioSink sink = { 0 };
	sink.header_cb = audio_header_cb;
	sink.frame_cb = audio_frame_cb;
	chiaki_session_set_audio_sink(&session, &sink);
	if(chiaki_session_start(&session) != CHIAKI_ERR_SUCCESS)
	{
		put_error("couldn't connect to the console");
		chiaki_session_fini(&session);
		return 1;
	}
	pthread_t in;
	pthread_create(&in, NULL, input_thread, NULL);
	pthread_detach(in);
	chiaki_session_join(&session);
	chiaki_session_fini(&session);
	return 0;
}

static bool is_ps5(const char *s) { return s && strcmp(s, "ps5") == 0; }

int main(int argc, char *argv[])
{
	chiaki_log_init(&logger, CHIAKI_LOG_ALL & ~CHIAKI_LOG_VERBOSE, log_cb, NULL);
	if(chiaki_lib_init() != CHIAKI_ERR_SUCCESS)
		return fail("couldn't start chiaki");
	if(argc >= 3 && strcmp(argv[1], "status") == 0)
		return cmd_status(argv[2]);
	if(argc >= 4 && strcmp(argv[1], "regist") == 0)
		return cmd_regist(argv[2], is_ps5(argv[3]));
	if(argc >= 4 && strcmp(argv[1], "wake") == 0)
		return cmd_wake(argv[2], is_ps5(argv[3]));
	if(argc >= 6 && strcmp(argv[1], "play") == 0)
		return cmd_play(argv[2], is_ps5(argv[3]), atoi(argv[4]), atoi(argv[5]));
	fprintf(stderr, "usage: matinee-psremote status|regist|wake|play <host> [ps4|ps5] [height fps]\n");
	return 2;
}
