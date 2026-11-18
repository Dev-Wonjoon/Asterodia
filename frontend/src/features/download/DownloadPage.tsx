import { useEffect, useRef, useState } from "react";
import { getApi } from "@renderer/shared/api";
import type { EngineStatus, EngineInstallProgress } from "@contracts/types";
import { ListDetailLayout } from "@renderer/shared/components/layout/ListDetailLayout";
import { useDownloadViewModel } from "./useDownloadViewModel";
import { DownloadQueue } from "./components/DownloadQueue";
import { DownloadDetailPane } from "./components/DownloadDetailPane";

export function DownloadPage(): React.JSX.Element | null {
    const vm = useDownloadViewModel();
    const { closeDetail } = vm;
    useEffect(() => () => closeDetail(), [closeDetail]);
    const [engines, setEngines] = useState<Record<string, EngineStatus> | null>(
        null,
    );
    const [engineError, setEngineError] = useState<string | null>(null);

    const [installing, setInstalling] = useState<string | null>(null);
    const installPending = useRef(false);
    const [installProgress, setInstallProgress] =
        useState<EngineInstallProgress | null>(null);

    const activeInstallId = useRef<string | null>(null);

    const handleInstall = async (name: string): Promise<void> => {
        if (installPending.current) return;

        const installId = crypto.randomUUID();

        installPending.current = true;
        activeInstallId.current = installId;

        setInstalling(name);
        setEngineError(null);
        setInstallProgress({
            installId,
            engine: name,
            phase: "preparing",
            downloaded: 0,
            total: null,
        });

        let unsubscribe: (() => void) | undefined;

        try {
            // 설치 요청 전에 구독한다.
            unsubscribe = getApi().onEngineInstallProgress((progress) => {
                if (
                    progress.installId !== activeInstallId.current ||
                    progress.engine !== name
                ) {
                    return;
                }

                setInstallProgress((current) => {
                    if (
                        current?.installId !== installId ||
                        current.phase === "completed" ||
                        current.phase === "failed"
                    ) {
                        return current;
                    }

                    return progress;
                });
            });

            const success = await getApi().installEngine(name, installId);

            if (!success) {
                throw new Error("설치에 실패했습니다.");
            }

            // 최종 이벤트 수신 시점과 관계없이 성공 결과를 반영한다.
            setInstallProgress((current) =>
                current?.installId === installId
                    ? { ...current, phase: "completed" }
                    : current,
            );

            const statuses = await getApi().getEngineStatus();
            setEngines(statuses);

            const installed = statuses[name];

            if (!installed?.installed) {
                throw new Error(
                    installed?.error ?? "설치 후 엔진을 확인하지 못했습니다.",
                );
            }
        } catch (error: unknown) {
            const message =
                error instanceof Error
                    ? error.message
                    : typeof error === "string"
                      ? error
                      : "엔진을 설치하지 못했습니다.";

            setEngineError(`${name}: ${message}`);
        } finally {
            unsubscribe?.();
            activeInstallId.current = null;
            installPending.current = false;
            setInstalling(null);
        }
    };

    useEffect(() => {
        let disposed = false;

        void getApi()
            .getEngineStatus()
            .then((status) => {
                if (!disposed) setEngines(status);
            })
            .catch((error: unknown) => {
                if (disposed) return;

                setEngineError(
                    error instanceof Error
                        ? error.message
                        : "엔진 상태를 확인하지 못했습니다.",
                );
            });

        return () => {
            disposed = true;
        };
    }, []);
    return (
        <ListDetailLayout
            detailLabel="다운로드 상세"
            detailKey={vm.detail.jobId}
            onClose={vm.closeDetail}
            detail={
                vm.detail.jobId !== null ? <DownloadDetailPane /> : undefined
            }
        >
            <div className="space-y-5">
                <header className="flex flex-wrap items-center justify-between gap-3">
                    <div>
                        <h1 className="text-lg font-semibold">다운로드</h1>
                        <p className="mt-1 text-sm text-[var(--text-muted)]">
                            작업 상태를 확인하고 취소하거나 다시 실행하세요.
                        </p>
                    </div>
                    <button
                        type="button"
                        disabled={vm.loading}
                        onClick={() => void vm.refresh()}
                        className="rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40"
                    >
                        {vm.loading ? "조회 중" : "새로고침"}
                    </button>
                </header>
                <section
                    aria-label="엔진 상태"
                    aria-live="polite"
                    aria-busy={installing !== null}
                    className="space-y-3 rounded-xl border border-[var(--border-line)] p-4 text-sm"
                >
                    <h2 className="font-medium">엔진 상태</h2>

                    {engineError && (
                        <p className="text-red-500">{engineError}</p>
                    )}

                    {engines === null
                        ? !engineError && <p>엔진 확인 중…</p>
                        : Object.entries(engines).map(([name, status]) => (
                              <div
                                  key={name}
                                  className="flex items-center justify-between gap-4"
                              >
                                  <div className="min-w-0 flex-1">
                                      <p className="font-medium">{name}</p>
                                      <p
                                          className={`mt-1 break-words text-xs ${
                                              status.installed ||
                                              installing === name
                                                  ? "text-[var(--text-muted)]"
                                                  : "text-red-500"
                                          }`}
                                      >
                                          {installing === name &&
                                              installProgress?.phase ===
                                                  "downloading" && (
                                                  <progress
                                                      aria-label={`${name} 다운로드 진행률`}
                                                      max={100}
                                                      value={engineDownloadPercent(
                                                          installProgress,
                                                      )}
                                                      className="mt-2 h-2 w-full accent-[var(--color-primary)]"
                                                  />
                                              )}
                                          {installing === name
                                              ? engineInstallText(
                                                    installProgress,
                                                )
                                              : status.installed
                                                ? status.version
                                                : (status.error ??
                                                  "사용할 수 없습니다.")}
                                      </p>
                                  </div>

                                  {!status.installed && (
                                      <button
                                          type="button"
                                          disabled={installing !== null}
                                          onClick={() =>
                                              void handleInstall(name)
                                          }
                                          className="shrink-0 rounded-lg bg-[var(--color-primary)] px-3 py-2 text-xs text-white disabled:opacity-40"
                                      >
                                          {installing === name
                                              ? "설치 중…"
                                              : "설치"}
                                      </button>
                                  )}
                              </div>
                          ))}
                </section>
                <DownloadQueue />
            </div>
        </ListDetailLayout>
    );
}

function engineDownloadPercent(
    progress: EngineInstallProgress | null,
): number | undefined {
    if (
        progress?.phase !== "downloading" ||
        progress.total === null ||
        progress.total <= 0
    ) {
        return undefined;
    }

    return Math.max(
        0,
        Math.min(100, Math.floor((progress.downloaded / progress.total) * 100)),
    );
}

function engineInstallText(progress: EngineInstallProgress | null): string {
    if (!progress) return "설치 준비 중…";

    if (progress.phase === "downloading") {
        const received = (progress.downloaded / 1_000_000).toFixed(1);
        const percent = engineDownloadPercent(progress);

        if (percent === undefined) {
            return `다운로드 중 · ${received} MB 받음`;
        }

        const total = ((progress.total ?? 0) / 1_000_000).toFixed(1);
        return `다운로드 ${percent}% · ${received} / ${total} MB`;
    }

    const labels: Record<string, string> = {
        preparing: "설치 준비 중…",
        extracting: "압축 해제 중…",
        verifying: "엔진 확인 중…",
        committing: "설치 파일 반영 중…",
        completed: "설치 완료",
        failed: "설치 실패",
    };

    return labels[progress.phase] ?? "설치 중…";
}
