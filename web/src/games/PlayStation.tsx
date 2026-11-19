import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, Gamepad2, Power, Trash2 } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { api, ApiError, type PlayStationConsole } from "../api";
import { Button, Field, inputClass } from "../components/ui";
import { GamePlayer } from "./GamePlayer";

/**
 * PlayStation Remote Play (preview): an admin signs in to PlayStation once
 * (for the account ID pairing needs), pairs a PS5 or PS4 on the home network
 * with the code it shows, then plays it like any game. Hidden when the
 * server can't (no matinee-psremote) or for someone who isn't an admin.
 */
export function PlayStation() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const setup = useQuery({
    queryKey: ["playstation"],
    queryFn: api.playStation,
    retry: false,
  });
  const [playing, setPlaying] = useState<PlayStationConsole>();
  const refresh = () => qc.invalidateQueries({ queryKey: ["playstation"] });

  if (!setup.data?.available) return null;
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-semibold">{t("games.ps.title")}</h2>
      <p className="text-sm text-muted">{t("games.ps.hint")}</p>
      {!setup.data.signedIn ? (
        <SignIn url={setup.data.signInUrl ?? ""} onDone={refresh} />
      ) : (
        <>
          {setup.data.consoles.map((c) => (
            <Console
              key={c.id}
              console={c}
              onPlay={() => setPlaying(c)}
              onChange={refresh}
            />
          ))}
          <Pair onDone={refresh} />
          <button
            type="button"
            className="text-sm text-muted underline-offset-2 hover:text-text hover:underline"
            onClick={() => api.forgetPlayStationAccount().then(refresh)}
          >
            {t("games.ps.signOut")}
          </button>
        </>
      )}
      {playing && (
        <GamePlayer
          name={playing.name}
          playstation={playing.id}
          onClose={() => {
            setPlaying(undefined);
            refresh();
          }}
        />
      )}
    </section>
  );
}

function errorText(e: unknown, fallback: string) {
  return e instanceof ApiError ? e.message : fallback;
}

function SignIn({ url, onDone }: { url: string; onDone: () => void }) {
  const { t } = useTranslation();
  const [byId, setById] = useState(false);
  const [value, setValue] = useState("");
  const save = useMutation({
    mutationFn: () =>
      api.setPlayStationAccount(
        byId ? { accountId: value.trim() } : { redirect: value.trim() },
      ),
    onSuccess: onDone,
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (value.trim()) save.mutate();
  };
  return (
    <form
      onSubmit={submit}
      className="space-y-3 rounded-xl border border-line/60 bg-surface/50 p-4"
    >
      <div className="font-medium">{t("games.ps.signInTitle")}</div>
      <p className="text-sm text-muted">
        {byId ? t("games.ps.accountIdHint") : t("games.ps.signInHint")}
      </p>
      {!byId && (
        <a
          href={url}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-2 text-sm font-medium text-accent hover:underline"
        >
          <ExternalLink className="size-4" />
          {t("games.ps.openSignIn")}
        </a>
      )}
      <div className="flex flex-col gap-2 sm:flex-row">
        <input
          className={inputClass}
          dir="ltr"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder={
            byId
              ? t("games.ps.accountIdPlaceholder")
              : t("games.ps.redirectPlaceholder")
          }
          aria-label={
            byId
              ? t("games.ps.accountIdPlaceholder")
              : t("games.ps.redirectPlaceholder")
          }
        />
        <Button
          type="submit"
          variant="primary"
          disabled={!value.trim() || save.isPending}
        >
          {t("save")}
        </Button>
      </div>
      {save.error && (
        <p className="text-sm text-danger">
          {errorText(save.error, t("somethingWentWrong"))}
        </p>
      )}
      <button
        type="button"
        className="text-sm text-muted underline-offset-2 hover:text-text hover:underline"
        onClick={() => {
          setById(!byId);
          setValue("");
          save.reset();
        }}
      >
        {byId ? t("games.ps.useSignIn") : t("games.ps.useAccountId")}
      </button>
    </form>
  );
}

function Console({
  console: c,
  onPlay,
  onChange,
}: {
  console: PlayStationConsole;
  onPlay: () => void;
  onChange: () => void;
}) {
  const { t } = useTranslation();
  const wake = useMutation({
    mutationFn: () => api.wakePlayStation(c.id),
    onSuccess: () => setTimeout(onChange, 20000),
  });
  const state = {
    ready: t("games.ps.stateReady"),
    standby: t("games.ps.stateStandby"),
    off: t("games.ps.stateOff"),
  }[c.state];
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-3 rounded-xl border border-line/60 bg-surface/50 p-4">
      <Gamepad2 className="size-6 shrink-0 text-accent" />
      <div className="min-w-48 flex-1">
        <div className="font-medium">{c.name}</div>
        <div className="text-sm text-muted">
          {[c.ps5 ? "PS5" : "PS4", state, c.app].filter(Boolean).join(" · ")}
        </div>
        {wake.error && (
          <p className="text-sm text-danger">
            {errorText(wake.error, t("somethingWentWrong"))}
          </p>
        )}
      </div>
      <div className="ms-auto flex items-center gap-2">
        {c.state === "standby" && (
          <Button
            icon={<Power className="size-4" />}
            disabled={wake.isPending || wake.isSuccess}
            onClick={() => wake.mutate()}
          >
            {wake.isSuccess ? t("games.ps.waking") : t("games.ps.wake")}
          </Button>
        )}
        <Button variant="primary" onClick={onPlay}>
          {t("games.play")}
        </Button>
        <button
          type="button"
          className="rounded-lg p-2 text-muted hover:bg-text/10 hover:text-text"
          aria-label={t("games.ps.remove", { name: c.name })}
          onClick={() => api.removePlayStation(c.id).then(onChange)}
        >
          <Trash2 className="size-4" />
        </button>
      </div>
    </div>
  );
}

function Pair({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation();
  const [host, setHost] = useState("");
  const [ps5, setPs5] = useState(true);
  const [pin, setPin] = useState("");
  const pair = useMutation({
    mutationFn: () =>
      api.pairPlayStation({ host: host.trim(), ps5, pin: pin.trim() }),
    onSuccess: () => {
      setPin("");
      onDone();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    pair.mutate();
  };
  return (
    <form
      onSubmit={submit}
      className="space-y-3 rounded-xl border border-line/60 bg-surface/50 p-4"
    >
      <div className="font-medium">{t("games.ps.pairTitle")}</div>
      <p className="text-sm text-muted">
        {ps5 ? t("games.ps.pairHintPS5") : t("games.ps.pairHintPS4")}
      </p>
      <div className="grid gap-3 sm:grid-cols-[1fr_auto_auto]">
        <Field label={t("games.ps.address")}>
          <input
            className={inputClass}
            dir="ltr"
            inputMode="decimal"
            value={host}
            onChange={(e) => setHost(e.target.value)}
            placeholder="192.168.1.20"
          />
        </Field>
        <Field label={t("games.ps.console")}>
          <select
            className={inputClass}
            value={ps5 ? "ps5" : "ps4"}
            onChange={(e) => setPs5(e.target.value === "ps5")}
          >
            {["ps5", "ps4"].map((m) => (
              <option key={m} value={m}>
                {m.toUpperCase()}
              </option>
            ))}
          </select>
        </Field>
        <div className="sm:w-36">
          <Field label={t("games.ps.code")}>
            <input
              className={inputClass}
              dir="ltr"
              inputMode="numeric"
              maxLength={8}
              value={pin}
              onChange={(e) => setPin(e.target.value.replace(/\D/g, ""))}
              placeholder="12345678"
            />
          </Field>
        </div>
      </div>
      {pair.error && (
        <p className="text-sm text-danger">
          {errorText(pair.error, t("somethingWentWrong"))}
        </p>
      )}
      <Button
        type="submit"
        variant="primary"
        disabled={!host.trim() || pin.length !== 8 || pair.isPending}
      >
        {pair.isPending ? t("games.ps.pairing") : t("games.ps.pair")}
      </Button>
    </form>
  );
}
