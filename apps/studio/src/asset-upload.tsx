import { useEffect, useRef, useState } from "react";
import type { Editor } from "./editor.js";
import { StudioError, type Api, type ContentOperation, type Upload } from "./types.js";
import { uploadLimit, uploadUrl } from "./content.js";
type Attempt = {
  file: File;
  seq: number;
  createKey: string;
  completeKey: string;
  payload?: Record<string, unknown>;
  upload?: Upload;
  putDone?: boolean;
};
export function AssetUpload({
  api,
  editor,
  disabled,
  target,
  expectedVersionId,
  onAttach,
  ru,
}: {
  api: Api;
  editor: Editor;
  disabled: boolean;
  target?: string | undefined;
  expectedVersionId?: string | undefined;
  onAttach: (ops: ContentOperation[], seq: number) => Promise<void>;
  ru: boolean;
}) {
  const [attempt, setAttempt] = useState<Attempt>(),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [version, setVersion] = useState(0);
  const life = useRef(new AbortController());
  useEffect(
    () => () => {
      life.current.abort();
      editor.formDirty("contentUpload", false);
    },
    [editor],
  );
  const config = editor.store.getState().session!.config;
  const reset = () => {
    setAttempt(undefined);
    setError("");
    setVersion((v) => v + 1);
    editor.formDirty("contentUpload", false);
  };
  const read = async () => {
    if (!attempt?.upload) return;
    setBusy(true);
    setError("");
    try {
      const upload = await api.uploadStatus(attempt.upload.assetId, life.current.signal);
      if (!life.current.signal.aborted) setAttempt({ ...attempt, upload });
    } catch (cause) {
      if (!life.current.signal.aborted)
        setError(cause instanceof StudioError ? cause.code : "UPLOAD_READ_FAILED");
    } finally {
      if (!life.current.signal.aborted) setBusy(false);
    }
  };
  const run = async () => {
    if (!attempt) return;
    setBusy(true);
    setError("");
    try {
      const file = attempt.file;
      if (!file.size || file.size > uploadLimit(file.type))
        throw new StudioError("UPLOAD_FILE_INVALID");
      if (!attempt.payload) {
        const digest = await crypto.subtle.digest("SHA-256", await file.arrayBuffer());
        attempt.payload = {
          filename: file.name,
          mimeType: file.type,
          size: file.size,
          sha256: [...new Uint8Array(digest)].map((v) => v.toString(16).padStart(2, "0")).join(""),
        };
      }
      if (life.current.signal.aborted) return;
      attempt.upload ??= await api.uploadCommand(
        "create-asset-upload",
        attempt.payload,
        attempt.createKey,
        life.current.signal,
      );
      if (life.current.signal.aborted) return;
      if (!attempt.putDone) {
        if (
          attempt.upload.status !== "pending" ||
          Date.parse(attempt.upload.expiresAt) <= Date.now()
        )
          throw new StudioError("ASSET_UPLOAD_EXPIRED");
        const response = await fetch(
          uploadUrl(attempt.upload.uploadUrl!, config.assetUploadOrigin),
          {
            method: "PUT",
            body: file,
            credentials: "omit",
            redirect: "error",
            referrerPolicy: "no-referrer",
            headers: { "Content-Type": file.type },
            signal: life.current.signal,
          },
        );
        if (!response.ok) throw new StudioError("UPLOAD_PUT_FAILED");
        attempt.putDone = true;
      }
      await api.uploadCommand(
        "complete-asset-upload",
        { assetId: attempt.upload.assetId },
        attempt.completeKey,
        life.current.signal,
      );
      const upload = await api.uploadStatus(attempt.upload.assetId, life.current.signal);
      if (!life.current.signal.aborted) setAttempt({ ...attempt, upload });
    } catch (cause) {
      if (!life.current.signal.aborted) {
        setAttempt({ ...attempt });
        setError(cause instanceof StudioError ? cause.code : "UPLOAD_NETWORK_UNCERTAIN");
      }
    } finally {
      if (!life.current.signal.aborted) setBusy(false);
    }
  };
  const stale = attempt && attempt.seq !== editor.store.getState().session!.changeset.seq;
  return (
    <section className="asset-upload" aria-label={ru ? "Загрузка ассета" : "Asset upload"}>
      <h3>{ru ? "Загрузить файл" : "Upload a file"}</h3>
      {!config.assetUploadOrigin && (
        <p>
          {ru
            ? "Для загрузки настройте assetUploadOrigin и S3 CORS. Можно выбирать существующие ассеты."
            : "Configure assetUploadOrigin and S3 CORS to upload. Existing assets can be selected."}
        </p>
      )}
      <input
        key={version}
        aria-label={ru ? "Файл ассета" : "Asset file"}
        type="file"
        disabled={disabled || busy || !!attempt || !config.assetUploadOrigin}
        onChange={(e) => {
          const file = e.target.files?.[0];
          if (file) {
            setAttempt({
              file,
              seq: editor.store.getState().session!.changeset.seq,
              createKey: crypto.randomUUID(),
              completeKey: crypto.randomUUID(),
            });
            editor.formDirty("contentUpload", true);
          }
        }}
      />
      {attempt && (
        <>
          <p>
            {attempt.file.name} · {attempt.file.size} bytes
          </p>
          <button
            type="button"
            disabled={
              disabled ||
              busy ||
              attempt.upload?.status === "ready" ||
              attempt.upload?.status === "rejected"
            }
            onClick={() => void run()}
          >
            {ru ? "Загрузить / повторить" : "Upload / retry"}
          </button>
          <button type="button" disabled={busy} onClick={reset}>
            {ru ? "Сбросить загрузку" : "Discard upload"}
          </button>
          {attempt.upload && (
            <>
              <p role="status">{attempt.upload.status}</p>
              <button type="button" disabled={disabled || busy} onClick={() => void read()}>
                {ru ? "Проверить обработку" : "Check processing"}
              </button>
              {attempt.upload.errorCode && (
                <p role="alert">
                  <code>{attempt.upload.errorCode}</code>
                </p>
              )}
            </>
          )}
          {stale && (
            <p role="alert">
              {ru
                ? "Черновик изменился; сбросьте загрузку перед новой правкой."
                : "The draft changed; discard the upload before another edit."}
            </p>
          )}
          {attempt.upload?.status === "ready" && (
            <button
              type="button"
              disabled={disabled || busy || !!stale}
              onClick={() => {
                editor.formDirty("contentUpload", false);
                void onAttach(
                  [
                    target
                      ? {
                          type: "asset.replaceFile",
                          target,
                          ...(expectedVersionId ? { expectedVersionId } : {}),
                          payload: { set: { fileHash: attempt.upload!.fileHash } },
                        }
                      : { type: "asset.create", payload: { uploadId: attempt.upload!.assetId } },
                  ],
                  attempt.seq,
                );
              }}
            >
              {target
                ? ru
                  ? "Заменить файл в черновике"
                  : "Replace file in draft"
                : ru
                  ? "Добавить ассет в черновик"
                  : "Add asset to draft"}
            </button>
          )}
        </>
      )}
      {error && (
        <p role="alert">
          <code>{error}</code>
        </p>
      )}
      <small>
        {ru
          ? "Готовый файл появится в CS только после явного добавления. Публикация выполняется отдельно."
          : "A ready file enters the CS only after explicit attachment. Publishing is a separate action."}
      </small>
    </section>
  );
}
