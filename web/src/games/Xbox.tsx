import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, Gamepad2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api, ApiError, type XboxConsole } from "../api";
import { Button } from "../components/ui";
import { GamePlayer } from "./GamePlayer";

/**
 * Xbox remote play (preview): an admin signs in to Xbox once, with the code
 * Microsoft's page asks for, then plays any of the account's consoles that
 * have remote features on, like any game. Hidden when the server can't or
 * for someone who isn't an admin.
 */
export function Xbox() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const setup = useQuery({
    queryKey: ["xbox"],
    queryFn: api.xbox,
    retry: false,
    // While a sign-in waits, look for it to finish.
    refetchInterval: (q) => (q.state.data?.signIn ? 3000 : false),
  });
  const [playing, setPlaying] = useState<XboxConsole>();
  const refresh = () => qc.invalidateQueries({ queryKey: ["xbox"] });
  const signIn = useMutation({ mutationFn: api.signInXbox, onSuccess: refresh });

  const data = setup.data;
  if (!data?.available) return null;
  const waiting = data.signIn;
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-semibold">{t("games.xbox.title")}</h2>
      <p className="text-sm text-muted">{t("games.xbox.hint")}</p>
      {!data.signedIn ? (
        <div className="space-y-3 rounded-xl border border-line/60 bg-surface/50 p-4">
          <div className="font-medium">{t("games.xbox.signInTitle")}</div>
          {waiting ? (
            <>
              <p className="text-sm text-muted">
                {t("games.xbox.codeHint", { url: "microsoft.com/link" })}
              </p>
              <div
                className="font-mono text-2xl font-semibold tracking-widest"
                dir="ltr"
              >
                {waiting.code}
              </div>
              <a
                href={waiting.url}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-2 text-sm font-medium text-accent hover:underline"
              >
                <ExternalLink className="size-4" />
                {t("games.xbox.openLink")}
              </a>
              <p className="text-sm text-muted">{t("games.xbox.waiting")}</p>
            </>
          ) : (
            <>
              <p className="text-sm text-muted">
                {t("games.xbox.signInHint")}
              </p>
              <Button
                variant="primary"
                disabled={signIn.isPending}
                onClick={() => signIn.mutate()}
              >
                {t("games.xbox.signIn")}
              </Button>
            </>
          )}
          {(signIn.error || data.signInError || data.error) && (
            <p className="text-sm text-danger">
              {signIn.error instanceof ApiError
                ? signIn.error.message
                : (data.signInError ?? data.error ?? t("somethingWentWrong"))}
            </p>
          )}
        </div>
      ) : (
        <>
          {data.consoles.map((c) => (
            <Console key={c.id} console={c} onPlay={() => setPlaying(c)} />
          ))}
          {data.error && <p className="text-sm text-danger">{data.error}</p>}
          {!data.error && data.consoles.length === 0 && (
            <p className="text-sm text-muted">{t("games.xbox.noConsoles")}</p>
          )}
          <p className="text-sm text-muted">{t("games.xbox.setupHint")}</p>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm text-muted">
            {data.gamertag && (
              <span>{t("games.xbox.signedInAs", { name: data.gamertag })}</span>
            )}
            <button
              type="button"
              className="underline-offset-2 hover:text-text hover:underline"
              onClick={() => api.signOutXbox().then(refresh)}
            >
              {t("games.xbox.signOut")}
            </button>
          </div>
        </>
      )}
      {playing && (
        <GamePlayer
          name={playing.name}
          xbox={playing.id}
          onClose={() => {
            setPlaying(undefined);
            refresh();
          }}
        />
      )}
    </section>
  );
}

/** XboxSeriesX → Xbox Series X. */
function model(type: string) {
  return type
    .replace(/^Xbox/, "Xbox ")
    .replace(/Series([XS])$/, "Series $1")
    .replace(/One([SX])$/, "One $1")
    .trim();
}

function Console({
  console: c,
  onPlay,
}: {
  console: XboxConsole;
  onPlay: () => void;
}) {
  const { t } = useTranslation();
  const state = {
    ready: t("games.xbox.stateReady"),
    standby: t("games.xbox.stateStandby"),
    off: t("games.xbox.stateOff"),
  }[c.state];
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-3 rounded-xl border border-line/60 bg-surface/50 p-4">
      <Gamepad2 className="size-6 shrink-0 text-accent" />
      <div className="min-w-48 flex-1">
        <div className="font-medium">{c.name}</div>
        <div className="text-sm text-muted">
          {[c.type && model(c.type), state].filter(Boolean).join(" · ")}
        </div>
      </div>
      <Button
        variant="primary"
        className="ms-auto"
        disabled={c.state === "off"}
        onClick={onPlay}
      >
        {t("games.play")}
      </Button>
    </div>
  );
}
