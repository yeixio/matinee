import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell, BellRing, Copy, X } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import {
  api,
  type AskResult,
  type ComingSoonTitle,
  type Item,
  type TitleRequest,
} from "../api";
import { duration, episodeCode } from "../format";
import i18n from "../i18n";
import { RemotePoster, useRequestOptions } from "./Arr";
import { Row, RowItem } from "./Row";
import { Button, Field, inputClass, Spinner } from "./ui";

// What Box Office adds when Radarr or Sonarr goes to it: titles on their
// way, greyed out with a progress bar, and other editions and better copies
// of what's in the libraries. Without Box Office none of this shows.

const problem = (s: ComingSoonTitle["stage"]) =>
  s === "failed" || s === "quarantined" || s === "waitingToMove";

/** stageLabel says where a title on its way is, in words. */
export function stageLabel(x: ComingSoonTitle): string {
  if (problem(x.stage))
    return i18n.t(
      x.stage === "waitingToMove"
        ? "boxOffice.stage.waitingToMove"
        : "boxOffice.stage.problem",
    );
  return i18n.t(`boxOffice.stage.${x.stage}`);
}

function OnItsWayCard({ x }: { x: ComingSoonTitle }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const notify = useMutation({
    mutationFn: (on: boolean) => api.notifyMe(x, on),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["comingSoon"] }),
  });
  const episode =
    x.kind === "show" && x.episodeNumber
      ? episodeCode(x.seasonNumber, x.episodeNumber)
      : "";
  const detail = [episode, x.edition].filter(Boolean).join(" · ");
  const bar =
    x.stage === "searching" ? 0 : Math.max(2, Math.min(100, x.percent));
  const poster = (
    <div className="relative">
      <RemotePoster
        src={x.poster}
        title={x.title}
        className={`rounded-xl ring-1 ring-text/5 grayscale ${problem(x.stage) ? "opacity-40" : "opacity-60"}`}
      />
      <div
        className="absolute inset-x-2 bottom-2 h-1.5 overflow-hidden rounded-full bg-black/50"
        role="progressbar"
        aria-label={stageLabel(x)}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(x.percent)}
      >
        <div
          className={`h-full rounded-full transition-[width] duration-700 ${problem(x.stage) ? "bg-danger" : "bg-accent"} ${x.stage === "searching" ? "w-1/3 animate-pulse" : ""}`}
          style={x.stage === "searching" ? undefined : { width: `${bar}%` }}
        />
      </div>
    </div>
  );
  return (
    <div className="animate-fade">
      {x.itemId ? (
        <Link to={`/item/${x.itemId}`} className="block">
          {poster}
        </Link>
      ) : (
        poster
      )}
      <div className="mt-2 px-0.5">
        <div className="truncate text-sm font-medium">{x.title}</div>
        {detail && <div className="truncate text-xs text-muted">{detail}</div>}
        <div className="truncate text-xs text-muted" title={x.message}>
          <span className={problem(x.stage) ? "text-danger" : "text-accent"}>
            {stageLabel(x)}
          </span>
          {x.etaSeconds && !problem(x.stage)
            ? ` · ${t("boxOffice.eta", { time: duration(x.etaSeconds * 1000) })}`
            : ""}
        </div>
        {!x.requested && !x.itemId && (
          <button
            type="button"
            className="mt-1 inline-flex items-center gap-1 text-xs text-muted hover:text-text disabled:opacity-50"
            disabled={notify.isPending}
            onClick={() => notify.mutate(!x.notifyMe)}
            aria-pressed={!!x.notifyMe}
          >
            {x.notifyMe ? (
              <BellRing className="size-3.5 text-accent" />
            ) : (
              <Bell className="size-3.5" />
            )}
            {x.notifyMe ? t("boxOffice.notifying") : t("boxOffice.notifyMe")}
          </button>
        )}
      </div>
    </div>
  );
}

/** OnItsWayRow shows Home what Box Office is downloading, greyed out with a progress bar. */
export function OnItsWayRow() {
  const { t } = useTranslation();
  const { data: opts } = useRequestOptions();
  const { data } = useQuery({
    queryKey: ["comingSoon"],
    queryFn: api.comingSoon,
    enabled: !!opts?.boxOffice?.progress,
    // Live, while it's open: Box Office's progress moves.
    refetchInterval: (q) =>
      q.state.data?.titles.some(
        (x) => !problem(x.stage) && x.stage !== "imported",
      )
        ? 10_000
        : 60_000,
  });
  if (!data?.available || !data.titles.length) return null;
  return (
    <Row title={t("boxOffice.onItsWay")}>
      {data.titles.slice(0, 30).map((x) => (
        <RowItem key={`${x.kind}:${x.tmdbId ?? 0}:${x.tvdbId ?? 0}`}>
          <OnItsWayCard x={x} />
        </RowItem>
      ))}
    </Row>
  );
}

function askedMessage(r: AskResult): string {
  return i18n.t(
    r.state === "pending"
      ? "boxOffice.copies.pending"
      : "boxOffice.copies.sent",
  );
}

/** OtherCopies offers, with Box Office, other editions of a movie and better or replacement copies. */
export function OtherCopies({ item }: { item: Item }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: opts } = useRequestOptions();
  const bo = opts?.boxOffice;
  const [open, setOpen] = useState(false);
  const [bad, setBad] = useState(false);
  const [reason, setReason] = useState("");
  const [done, setDone] = useState("");
  const isMovie = item.kind === "movie";
  const editions = !!bo?.editions && isMovie;
  const upgrades = isMovie
    ? !!bo?.upgradeMovies
    : item.kind === "episode" && !!bo?.upgradeShows;
  const found = useQuery({
    queryKey: ["editions", item.id],
    queryFn: () => api.itemEditions(item.id),
    enabled: open && editions,
    staleTime: 10 * 60_000,
    retry: false,
  });
  const after = (r: AskResult) => {
    setDone(askedMessage(r));
    setBad(false);
    setReason("");
    qc.invalidateQueries({ queryKey: ["comingSoon"] });
    qc.invalidateQueries({ queryKey: ["requests"] });
    qc.invalidateQueries({ queryKey: ["requestOptions"] });
  };
  const edition = useMutation({
    mutationFn: (v: { edition: string; mode: "replace" | "alongside" }) =>
      api.askEdition(item.id, v.edition, v.mode),
    onSuccess: after,
  });
  const copy = useMutation({
    mutationFn: (v: { want: "better" | "replace"; reason?: string }) =>
      api.askCopy(item.id, v.want, v.reason),
    onSuccess: after,
  });
  if (!opts?.canRequest || (!editions && !upgrades)) return null;
  if (!open) {
    return (
      <Button
        variant="ghost"
        icon={<Copy className="size-4" />}
        onClick={() => setOpen(true)}
      >
        {t("boxOffice.copies.button")}
      </Button>
    );
  }
  const busy = edition.isPending || copy.isPending;
  const error = edition.error ?? copy.error;
  const submitBad = (e: FormEvent) => {
    e.preventDefault();
    copy.mutate({ want: "replace", reason });
  };
  return (
    <div className="w-full max-w-lg space-y-4 rounded-xl border border-line/60 bg-surface/90 p-4 backdrop-blur">
      <div className="flex items-center justify-between">
        <span className="font-medium">{t("boxOffice.copies.button")}</span>
        <button
          type="button"
          onClick={() => setOpen(false)}
          aria-label={t("close")}
          className="text-muted hover:text-text"
        >
          <X className="size-4" />
        </button>
      </div>
      {done && <p className="text-sm text-accent">{done}</p>}
      {error && <p className="text-sm text-danger">{error.message}</p>}
      {editions && (
        <div className="space-y-2">
          <h3 className="text-sm font-medium">
            {t("boxOffice.copies.editions")}
          </h3>
          {found.isLoading && (
            <p className="flex items-center gap-2 text-sm text-muted">
              <Spinner className="size-4" /> {t("boxOffice.copies.looking")}
            </p>
          )}
          {found.error && (
            <p className="text-sm text-danger">{found.error.message}</p>
          )}
          {found.data && !found.data.editions.length && (
            <p className="text-sm text-muted">{t("boxOffice.copies.none")}</p>
          )}
          {found.data?.editions.map((e) => (
            <div
              key={e.edition}
              className="flex flex-wrap items-center gap-2 rounded-lg border border-line/60 p-2"
            >
              <span className="min-w-0 grow basis-full sm:basis-0">
                <span className="block text-sm font-medium">{e.edition}</span>
                <span className="block text-xs text-muted">
                  {e.owned ? t("boxOffice.copies.owned") : e.quality}
                </span>
              </span>
              {!e.owned && (
                <>
                  <Button
                    disabled={busy}
                    onClick={() =>
                      edition.mutate({ edition: e.edition, mode: "alongside" })
                    }
                  >
                    {t("boxOffice.copies.keepBoth")}
                  </Button>
                  {found.data?.hasFile && (
                    <Button
                      variant="ghost"
                      disabled={busy}
                      onClick={() =>
                        edition.mutate({ edition: e.edition, mode: "replace" })
                      }
                    >
                      {t("boxOffice.copies.replace")}
                    </Button>
                  )}
                </>
              )}
            </div>
          ))}
        </div>
      )}
      {upgrades && !bad && (
        <div className="flex flex-wrap gap-2">
          <Button
            disabled={busy}
            onClick={() => copy.mutate({ want: "better" })}
          >
            {t("boxOffice.copies.better")}
          </Button>
          <Button variant="ghost" disabled={busy} onClick={() => setBad(true)}>
            {t("boxOffice.copies.bad")}
          </Button>
        </div>
      )}
      {upgrades && bad && (
        <form onSubmit={submitBad} className="space-y-3">
          <Field label={t("boxOffice.copies.reason")}>
            <textarea
              className={`${inputClass} h-20 py-2`}
              maxLength={500}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={t("boxOffice.copies.reasonExample")}
            />
          </Field>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={() => setBad(false)}>
              {t("cancel")}
            </Button>
            <Button variant="primary" disabled={busy}>
              {t("boxOffice.copies.send")}
            </Button>
          </div>
        </form>
      )}
      {bo?.asksNeedApproval && (
        <p className="text-xs text-muted">
          {t("boxOffice.copies.needsApproval")}
        </p>
      )}
    </div>
  );
}

/** askLabel says what a request with Box Office asks for, beyond a title; '' for a title. */
export function askLabel(r: TitleRequest): string {
  const episode = r.episodeNumber
    ? `${episodeCode(r.seasonNumber, r.episodeNumber)} · `
    : "";
  switch (r.ask) {
    case "edition":
      return i18n.t(
        r.mode === "replace"
          ? "boxOffice.ask.replaceEdition"
          : "boxOffice.ask.alongside",
        { edition: r.edition ?? "" },
      );
    case "better":
      return episode + i18n.t("boxOffice.ask.better");
    case "replace":
      return (
        episode +
        (r.reason
          ? i18n.t("boxOffice.ask.bad", { reason: r.reason })
          : i18n.t("boxOffice.ask.badNoReason"))
      );
  }
  return "";
}

/** ReplaceDamagedSetting lets admins have Box Office replace videos Matinee finds damaged. Off by default. */
export function ReplaceDamagedSetting() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: opts } = useRequestOptions();
  const { data } = useQuery({ queryKey: ["settings"], queryFn: api.settings });
  const save = useMutation({
    mutationFn: api.saveSettings,
    onSuccess: (s) => qc.setQueryData(["settings"], s),
  });
  if (!opts?.boxOffice?.fixMovies && !opts?.boxOffice?.fixShows) return null;
  return (
    <label className="flex cursor-pointer items-start gap-3 rounded-xl border border-line/60 bg-surface/50 p-3">
      <input
        type="checkbox"
        className="mt-1 accent-[var(--color-accent)]"
        checked={!!data?.boxOfficeReplaceDamaged}
        disabled={!data || save.isPending}
        onChange={(e) =>
          save.mutate({ boxOfficeReplaceDamaged: e.target.checked })
        }
      />
      <span>
        <span className="block text-sm font-medium">
          {t("boxOffice.settings.replaceDamaged")}
        </span>
        <span className="block text-sm text-muted">
          {t("boxOffice.settings.replaceDamagedDetail")}
        </span>
      </span>
    </label>
  );
}

/** AsksApprovalSetting lets admins make editions and better copies always wait for them. */
export function AsksApprovalSetting() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: opts } = useRequestOptions();
  const { data } = useQuery({ queryKey: ["settings"], queryFn: api.settings });
  const save = useMutation({
    mutationFn: api.saveSettings,
    onSuccess: (s) => {
      qc.setQueryData(["settings"], s);
      qc.invalidateQueries({ queryKey: ["requestOptions"] });
    },
  });
  if (!opts?.boxOffice?.editions && !opts?.boxOffice?.upgrades) return null;
  return (
    <label className="flex cursor-pointer items-start gap-3 rounded-xl border border-line/60 bg-surface/50 p-3">
      <input
        type="checkbox"
        className="mt-1 accent-[var(--color-accent)]"
        checked={!!data?.boxOfficeAsksNeedApproval}
        disabled={!data || save.isPending}
        onChange={(e) =>
          save.mutate({ boxOfficeAsksNeedApproval: e.target.checked })
        }
      />
      <span>
        <span className="block text-sm font-medium">
          {t("boxOffice.settings.approval")}
        </span>
        <span className="block text-sm text-muted">
          {t("boxOffice.settings.approvalDetail")}
          {(opts?.boxOffice?.fixMovies || opts?.boxOffice?.fixShows) &&
            ` ${t("boxOffice.settings.approvalReports")}`}
        </span>
      </span>
    </label>
  );
}
