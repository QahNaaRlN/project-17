import { useEffect, useMemo, useState } from "react";
import { useStore } from "zustand";
import {
  DndContext,
  PointerSensor,
  useSensor,
  useSensors,
  useDraggable,
  useDroppable,
  pointerWithin,
  MeasuringStrategy,
} from "@dnd-kit/core";
import { generateNodeId, type NestedNode } from "@cms/ir";
import type { Editor } from "./editor.js";
import { layers, type Layer } from "./fields.js";
import {
  canRemove,
  createBlock,
  destinations,
  duplicate,
  insertion,
  location,
  subtree,
  palette,
  type Place,
} from "./structure.js";
import { StudioError } from "./types.js";
import { ComponentExtractDialog } from "./component-extract-panel.js";
import { componentOf } from "./composed.js";

const copy = {
  ru: {
    layers: "Слои",
    palette: "Палитра",
    search: "Поиск компонентов",
    component: "Компонент",
    add: "Добавить блок",
    destination: "Место вставки",
    children: "Дочерние элементы",
    position: "позиция",
    move: "Переместить",
    duplicate: "Дублировать",
    remove: "Удалить",
    structure: "Структура",
    none: "Нет доступных мест вставки",
    setup: "Требует дополнительной настройки",
    select: "Выберите элемент",
    drop: "Перетащите в одно из разрешённых мест ниже",
    moving: "Перемещение блока",
    draggable: "Перемещаемый элемент",
    keyboard:
      "Для перемещения выберите место вставки в панели «Структура» и нажмите «Переместить».",
    cancelled: "Перемещение отменено",
    locked: "Элемент заблокирован",
    defaults: "Текст нового блока можно изменить в свойствах.",
    server: "Ограничения проекта и доступ к правке проверяются при сохранении.",
    extract: "Сохранить как компонент",
    extractRoot: "Корень страницы нельзя сохранить как компонент.",
    extractPage: "Сохранить как компонент можно только блок страницы.",
    openComponent: "Открыть компонент",
    componentDoc: "Компонент",
    componentHint:
      "Изменения компонента видны в превью страницы с экземпляром. Превью самого компонента нет.",
    backToPage: "Вернуться к странице",
    certified: "Компонент сертифицирован: правка снимет сертификацию.",
  },
  en: {
    layers: "Layers",
    palette: "Palette",
    search: "Search components",
    component: "Component",
    add: "Add block",
    destination: "Insertion destination",
    children: "Children",
    position: "position",
    move: "Move",
    duplicate: "Duplicate",
    remove: "Delete",
    structure: "Structure",
    none: "No available destinations",
    setup: "Requires additional setup",
    select: "Select an element",
    drop: "Drag to an allowed destination below",
    moving: "Moving block",
    draggable: "Movable element",
    keyboard: "To move a block, choose an insertion destination in Structure and press Move.",
    cancelled: "Move cancelled",
    locked: "Element is locked",
    defaults: "Edit the new block's text in Properties.",
    server: "Project restrictions and edit permissions are checked when saving.",
    extract: "Save as component",
    extractRoot: "The page root cannot become a component.",
    extractPage: "Only a page block can become a component.",
    openComponent: "Open component",
    componentDoc: "Component",
    componentHint:
      "Component changes are visible in the preview of the page with the instance. There is no component preview.",
    backToPage: "Back to page",
    certified: "The component is certified: editing removes certification.",
  },
};
const placeKey = (p: Place) => JSON.stringify(p);
function LayerButton({
  item,
  selected,
  enabled,
  editor,
  locked,
  description,
}: {
  item: Layer;
  selected: string | null;
  enabled: boolean;
  editor: Editor;
  locked: string;
  description: string;
}) {
  const drag = useDraggable({ id: item.id, disabled: !enabled });
  return (
    <button
      ref={drag.setNodeRef}
      {...drag.attributes}
      {...drag.listeners}
      aria-disabled={undefined}
      aria-roledescription={enabled ? description : undefined}
      aria-describedby={enabled ? drag.attributes["aria-describedby"] : undefined}
      role="treeitem"
      aria-level={item.depth}
      aria-selected={selected === item.id}
      aria-expanded={
        editor.store.getState().document &&
        layers(editor.store.getState().document!).some((n) => n.parent === item.id)
          ? true
          : undefined
      }
      tabIndex={selected === item.id ? 0 : -1}
      style={{ paddingLeft: 12 + (item.depth - 1) * 16, opacity: drag.isDragging ? 0.5 : 1 }}
      title={item.id}
      onClick={() => editor.select(item.id)}
    >
      <span className="node-icon">◇</span>
      {item.label}
      {item.locked && <span aria-label={locked}> · 🔒</span>}
      {/* STU-372: a component placeholder waits for its request. */}
      {typeof editor.store.getState().document?.nodes[item.id]?.pending === "string" && (
        <span className="pending-badge">
          {" · "}
          {editor.store.getState().locale === "ru" ? "ожидает компонент" : "awaiting component"}
        </span>
      )}
    </button>
  );
}
function DropPlace({ place, label }: { place: Place; label: string }) {
  const drop = useDroppable({ id: placeKey(place) });
  return (
    <div ref={drop.setNodeRef} className={"drop-place" + (drop.isOver ? " active" : "")}>
      {label}
    </div>
  );
}
export function StructurePanel({ editor }: { editor: Editor }) {
  const state = useStore(editor.store),
    t = copy[state.locale],
    doc = state.document,
    session = state.session;
  const [type, setType] = useState("Stack"),
    [search, setSearch] = useState(""),
    [values, setValues] = useState<Record<string, string>>({}),
    [addPlace, setAddPlace] = useState(""),
    [movePlace, setMovePlace] = useState(""),
    [dragging, setDragging] = useState<string | null>(null),
    [notice, setNotice] = useState(""),
    [extracting, setExtracting] = useState<string | null>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 8 } }));
  const tree = useMemo(() => (doc ? layers(doc) : []), [doc]);
  const items = session ? palette(session.manifest) : [];
  const item = items.find((i) => i.type === type);
  const previewTree = useMemo<NestedNode>(
    () => ({ id: generateNodeId(new Set(Object.keys(doc?.nodes ?? {}))), type }),
    [type, doc],
  );
  const addPlaces = useMemo(
    () =>
      doc && session && item?.supported ? destinations(doc, session.manifest, previewTree) : [],
    [doc, session, item?.supported, previewTree],
  );
  const movePlaces = useMemo(
    () =>
      doc && session && state.selected
        ? destinations(doc, session.manifest, subtree(doc, state.selected), state.selected)
        : [],
    [doc, session, state.selected],
  );
  const dropPlaces = useMemo(
    () =>
      doc && session && dragging && doc.nodes[dragging]
        ? destinations(doc, session.manifest, subtree(doc, dragging), dragging)
        : [],
    [doc, session, dragging],
  );
  const enabled =
    state.status === "ready" &&
    !Object.entries(state.dirtyFields).some(([key, value]) => key !== "structureDrag" && value) &&
    session?.changeset.state === "open" &&
    session.review.canEdit;
  const removable =
    !!doc && !!session && !!state.selected && canRemove(doc, state.selected, session.manifest);
  function label(p: Place) {
    return `${doc!.nodes[p.parentId]!.name ?? doc!.nodes[p.parentId]!.type} · ${p.slot || t.children} · ${t.position} ${p.index + 1}`;
  }
  function chosen(places: Place[], value: string, parent?: string) {
    return (
      places.find((p) => placeKey(p) === value) ??
      places.find((p) => p.parentId === parent) ??
      places[0]
    );
  }
  const addTarget =
      addPlaces.find((p) => placeKey(p) === addPlace) ??
      addPlaces.filter((p) => p.parentId === state.selected).at(-1) ??
      addPlaces.filter((p) => p.parentId === doc?.root).at(-1) ??
      addPlaces[0],
    moveTarget = chosen(movePlaces, movePlace);
  function report(action: () => void) {
    try {
      action();
    } catch (cause) {
      editor.error(cause instanceof StudioError ? cause.code : "STRUCTURE_INVALID");
    }
  }
  function move(place: Place, nodeId = state.selected) {
    if (!enabled || !doc || !session || !nodeId) return;
    const allowed = destinations(doc, session.manifest, subtree(doc, nodeId), nodeId);
    if (!allowed.some((p) => placeKey(p) === placeKey(place))) return;
    void editor.apply(
      [{ type: "node.move", target: session.record!.id, payload: { ...place, nodeId } }],
      nodeId,
    );
  }
  function remove() {
    if (!enabled || !removable || !state.selected || !doc || !session) return;
    const parent = location(doc, state.selected)!.parentId;
    void editor.apply(
      [{ type: "node.remove", target: session.record!.id, payload: { nodeId: state.selected } }],
      parent,
    );
  }
  function clone() {
    if (!enabled || !state.selected || !doc || !session) return;
    const from = location(doc, state.selected);
    if (!from) return;
    const tree = duplicate(doc, state.selected, session.manifest),
      place = { ...from, index: from.index + 1 };
    if (!destinations(doc, session.manifest, tree).some((p) => placeKey(p) === placeKey(place)))
      return;
    void editor.apply([insertion(session.record!.id, place, tree)], tree.id);
  }
  const duplicable = useMemo(() => {
    if (!doc || !session || !state.selected) return false;
    const from = location(doc, state.selected);
    if (!from) return false;
    // Eligibility uses a fresh ID tree; insertion never reuses the source IDs.
    return destinations(
      doc,
      session.manifest,
      duplicate(doc, state.selected, session.manifest),
    ).some(
      (p) => p.parentId === from.parentId && p.slot === from.slot && p.index === from.index + 1,
    );
  }, [doc, session, state.selected]);
  const isPage = doc?.kind === "page";
  const extractable =
    !!enabled && isPage && !!state.selected && state.selected !== doc?.root && !dragging;
  const selectedComponent = componentOf(state.selected ? doc?.nodes[state.selected] : undefined);
  useEffect(() => {
    const key = (event: KeyboardEvent) => {
      // STU-024: Ctrl/Cmd+Alt+K opens the wizard for the selected block.
      if (
        !event.defaultPrevented &&
        event.altKey &&
        (event.ctrlKey || event.metaKey) &&
        event.code === "KeyK" &&
        extractable
      ) {
        event.preventDefault();
        setExtracting(state.selected);
        return;
      }
      if (event.defaultPrevented || event.isComposing || event.altKey || !enabled || dragging)
        return;
      if (
        event.target instanceof Element &&
        event.target.closest(
          'input,textarea,select,[contenteditable]:not([contenteditable="false"]),[role="textbox"]',
        )
      )
        return;
      if (
        (event.ctrlKey || event.metaKey) &&
        !event.shiftKey &&
        (event.key.toLowerCase() === "d" || event.code === "KeyD")
      ) {
        event.preventDefault();
        report(clone);
      }
      if (
        event.key === "Delete" &&
        !event.ctrlKey &&
        !event.metaKey &&
        event.target instanceof Element &&
        event.target.closest('[role="tree"]')
      ) {
        event.preventDefault();
        remove();
      }
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  });
  return (
    <aside className="layers">
      {extracting && doc && session && (
        <ComponentExtractDialog
          editor={editor}
          nodeId={extracting}
          onClose={() => setExtracting(null)}
        />
      )}
      {doc?.kind === "component" && (
        <section aria-label={t.componentDoc} className="structure-panel component-document">
          <h2>
            {t.componentDoc}: {typeof doc.meta?.title === "string" ? doc.meta.title : ""}
          </h2>
          <p className="hint">{t.componentHint}</p>
          {state.pageReturn && (
            <button
              type="button"
              disabled={state.status === "saving"}
              onClick={() => void editor.returnToPage()}
            >
              {t.backToPage}
            </button>
          )}
        </section>
      )}
      <h2>{t.layers}</h2>
      <DndContext
        sensors={sensors}
        accessibility={{
          screenReaderInstructions: { draggable: t.keyboard },
          announcements: {
            onDragStart: () => t.moving,
            onDragOver: ({ over }) => {
              const target = dropPlaces.find((p) => placeKey(p) === over?.id);
              return target ? label(target) : t.none;
            },
            onDragEnd: ({ over }) => (over ? t.moving : t.cancelled),
            onDragCancel: () => t.cancelled,
          },
        }}
        measuring={{ droppable: { strategy: MeasuringStrategy.Always } }}
        collisionDetection={pointerWithin}
        onDragStart={(event) => {
          editor.formDirty("structureDrag", true);
          setDragging(String(event.active.id));
          setNotice(t.moving);
          editor.select(String(event.active.id));
        }}
        onDragCancel={() => {
          editor.formDirty("structureDrag", false);
          setDragging(null);
          setNotice(t.cancelled);
        }}
        onDragEnd={(event) => {
          const place = dropPlaces.find((p) => placeKey(p) === event.over?.id);
          if (place) report(() => move(place, String(event.active.id)));
          editor.formDirty("structureDrag", false);
          setDragging(null);
          setNotice(place ? "" : t.cancelled);
        }}
      >
        <div
          role="tree"
          aria-label={t.layers}
          onKeyDown={(event) => {
            if (event.ctrlKey || event.metaKey || event.altKey || dragging) return;
            const index = tree.findIndex((n) => n.id === state.selected);
            let next: string | undefined;
            if (event.key === "ArrowDown") next = tree[Math.min(index + 1, tree.length - 1)]?.id;
            if (event.key === "ArrowUp") next = tree[Math.max(0, index - 1)]?.id;
            if (event.key === "Home") next = tree[0]?.id;
            if (event.key === "End") next = tree.at(-1)?.id;
            if (event.key === "ArrowLeft" || event.key === "Escape")
              next = tree[index]?.parent ?? undefined;
            if (event.key === "ArrowRight" && tree[index + 1]?.parent === state.selected)
              next = tree[index + 1]?.id;
            if (next) {
              event.preventDefault();
              editor.select(next);
              const buttons = event.currentTarget.querySelectorAll<HTMLElement>("[role=treeitem]");
              buttons[tree.findIndex((n) => n.id === next)]?.focus();
            }
          }}
        >
          {tree.map((n) => (
            <LayerButton
              key={n.id}
              item={n}
              selected={state.selected}
              editor={editor}
              locked={t.locked}
              description={t.draggable}
              enabled={!!enabled && !!session && !!doc && !!location(doc, n.id) && !n.locked}
            />
          ))}
        </div>
        {dragging && (
          <section aria-label={t.destination} className="drop-places">
            <p>{t.drop}</p>
            {dropPlaces.length ? (
              dropPlaces.map((p) => <DropPlace key={placeKey(p)} place={p} label={label(p)} />)
            ) : (
              <p>{t.none}</p>
            )}
          </section>
        )}
      </DndContext>
      <p role="status" className="hint">
        {notice}
      </p>
      {doc && session && (
        <>
          <section aria-label={t.structure} className="structure-panel">
            <h2>{t.structure}</h2>
            <div className="structure-actions">
              <button disabled={!enabled || !duplicable} onClick={() => report(clone)}>
                {t.duplicate}
              </button>
              <button disabled={!enabled || !removable} onClick={remove}>
                {t.remove}
              </button>
              {isPage && (
                <button
                  disabled={!extractable}
                  aria-keyshortcuts="Control+Alt+K Meta+Alt+K"
                  onClick={() => setExtracting(state.selected)}
                >
                  {t.extract}
                </button>
              )}
              {selectedComponent && (
                <button
                  disabled={
                    state.status !== "ready" || Object.values(state.dirtyFields).some(Boolean)
                  }
                  onClick={() => {
                    const ref = doc.nodes[state.selected!]?.ref?.component;
                    if (ref) void editor.openComponent(ref);
                  }}
                >
                  {t.openComponent}
                </button>
              )}
            </div>
            {isPage && state.selected === doc.root && <p className="hint">{t.extractRoot}</p>}
            <label>
              {t.destination}
              <select
                aria-label={t.destination}
                disabled={!enabled || !moveTarget}
                value={moveTarget ? placeKey(moveTarget) : ""}
                onChange={(e) => setMovePlace(e.target.value)}
              >
                {!movePlaces.length && <option value="">{t.none}</option>}
                {movePlaces.map((p) => (
                  <option key={placeKey(p)} value={placeKey(p)}>
                    {label(p)}
                  </option>
                ))}
              </select>
            </label>
            <button
              disabled={!enabled || !moveTarget}
              onClick={() => moveTarget && report(() => move(moveTarget))}
            >
              {t.move}
            </button>
          </section>
          <section aria-label={t.palette} className="structure-panel">
            <h2>{t.palette}</h2>
            <label>
              {t.search}
              <input value={search} onChange={(e) => setSearch(e.target.value)} />
            </label>
            <label>
              {t.component}
              <select
                aria-label={t.component}
                value={type}
                disabled={!enabled}
                onChange={(e) => {
                  setType(e.target.value);
                  setValues({});
                  setAddPlace("");
                }}
              >
                {items
                  .filter(
                    (i) =>
                      i.type === type ||
                      (i.type + " " + (i.definition.description ?? ""))
                        .toLowerCase()
                        .includes(search.toLowerCase()),
                  )
                  .map((i) => (
                    <option key={i.type} value={i.type} disabled={!i.supported}>
                      {i.type}
                      {!i.supported ? " · " + t.setup : ""}
                    </option>
                  ))}
              </select>
            </label>
            {item?.definition.description && <p className="hint">{item.definition.description}</p>}
            {item?.required.map(([name, schema]) => (
              <label key={type + name}>
                {name}
                {schema.type === "enum" || schema.type === "boolean" ? (
                  <select
                    value={values[name] ?? ""}
                    disabled={!enabled}
                    onChange={(e) => setValues({ ...values, [name]: e.target.value })}
                  >
                    <option value="">—</option>
                    {(schema.type === "boolean" ? ["true", "false"] : schema.values).map((v) => (
                      <option key={v}>{v}</option>
                    ))}
                  </select>
                ) : (
                  <input
                    value={values[name] ?? ""}
                    disabled={!enabled}
                    onChange={(e) => setValues({ ...values, [name]: e.target.value })}
                    type={schema.type === "number" ? "number" : "text"}
                  />
                )}
              </label>
            ))}
            <label>
              {t.destination}
              <select
                aria-label={t.palette + ": " + t.destination}
                disabled={!enabled || !addTarget}
                value={addTarget ? placeKey(addTarget) : ""}
                onChange={(e) => setAddPlace(e.target.value)}
              >
                {!addPlaces.length && <option value="">{t.none}</option>}
                {addPlaces.map((p) => (
                  <option key={placeKey(p)} value={placeKey(p)}>
                    {label(p)}
                  </option>
                ))}
              </select>
            </label>
            <button
              disabled={!enabled || !addTarget}
              onClick={() =>
                report(() => {
                  if (!addTarget) return;
                  const block = createBlock(
                    type,
                    session.manifest,
                    session.record!.id,
                    state.locale,
                    values,
                    new Set(Object.keys(doc.nodes)),
                  );
                  if (
                    !destinations(doc, session.manifest, block.tree).some(
                      (p) => placeKey(p) === placeKey(addTarget),
                    )
                  )
                    throw new StudioError("STRUCTURE_INVALID");
                  void editor.apply(
                    [...block.operations, insertion(session.record!.id, addTarget, block.tree)],
                    block.tree.id,
                  );
                })
              }
            >
              {t.add}
            </button>
            <p className="hint">{t.defaults}</p>
            <p className="hint">{t.server}</p>
          </section>
        </>
      )}
    </aside>
  );
}
