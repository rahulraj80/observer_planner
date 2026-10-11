import { useEffect, useRef, useState } from "react";
import clsx from "clsx";
import { Spinner } from "./Spinner";

// CodeEditor: the platform's one code / config text editor (CodeMirror 6,
// operator ruling D-108). Used by the node dashboard's AI tool settings
// page for vendor config files (JSON, JSONC, TOML, YAML, Markdown) and
// available to web2 for the org policy editors.
//
// PURE like every shared primitive: no fetch, no router, no app state. The
// caller owns the text; diagnostics are data the caller passes (the server's
// hygiene findings with a line / column), drawn as CodeMirror lint marks.
//
// LAZY. CodeMirror is loaded on first mount through dynamic imports, so a
// page that never opens an editor never downloads it; a plain monospace
// fallback renders until it arrives (and stays if the import fails).
//
// THEMED from the design tokens (var(--bg-*), var(--fg-*), the tone
// colours), so it follows light / dark with the rest of the app.

export type CodeLanguage = "json" | "jsonc" | "toml" | "yaml" | "markdown" | "text";

export type CodeDiagnostic = {
  /** 1-based line; 0 or absent marks the first line. */
  line?: number;
  /** 1-based column; 0 or absent marks the whole line. */
  column?: number;
  message: string;
  severity: "error" | "warning" | "info";
};

export type CodeEditorProps = {
  value: string;
  onChange?: (v: string) => void;
  language: CodeLanguage;
  readOnly?: boolean;
  diagnostics?: CodeDiagnostic[];
  /** Accessible name of the editing surface. */
  ariaLabel: string;
  minHeight?: number;
  maxHeight?: number;
  className?: string;
};

// The CodeMirror modules, loaded once per page.
type CM = {
  view: typeof import("@codemirror/view");
  state: typeof import("@codemirror/state");
  commands: typeof import("@codemirror/commands");
  language: typeof import("@codemirror/language");
  lint: typeof import("@codemirror/lint");
  highlight: typeof import("@lezer/highlight");
};

let cmPromise: Promise<CM> | null = null;

function loadCM(): Promise<CM> {
  if (!cmPromise) {
    cmPromise = Promise.all([
      import("@codemirror/view"),
      import("@codemirror/state"),
      import("@codemirror/commands"),
      import("@codemirror/language"),
      import("@codemirror/lint"),
      import("@lezer/highlight"),
    ]).then(([view, state, commands, language, lint, highlight]) => ({
      view,
      state,
      commands,
      language,
      lint,
      highlight,
    }));
    cmPromise.catch(() => {
      cmPromise = null;
    });
  }
  return cmPromise;
}

// languageExtension loads the grammar for one language (a table, one row
// per language; unknown -> plain text).
async function languageExtension(cm: CM, lang: CodeLanguage) {
  switch (lang) {
    case "json": {
      const m = await import("@codemirror/lang-json");
      return m.json();
    }
    case "jsonc": {
      // lang-json's grammar has no comments; the legacy JavaScript mode in
      // JSON flavour tokenises them.
      const m = await import("@codemirror/legacy-modes/mode/javascript");
      return cm.language.StreamLanguage.define(m.json);
    }
    case "toml": {
      const m = await import("@codemirror/legacy-modes/mode/toml");
      return cm.language.StreamLanguage.define(m.toml);
    }
    case "yaml": {
      const m = await import("@codemirror/lang-yaml");
      return m.yaml();
    }
    case "markdown": {
      const m = await import("@codemirror/lang-markdown");
      return m.markdown();
    }
  }
  return [];
}

function themeExtensions(cm: CM, minHeight: number, maxHeight: number) {
  const t = cm.highlight.tags;
  const style = cm.language.HighlightStyle.define([
    { tag: [t.keyword, t.bool, t.null, t.atom], color: "var(--accent)" },
    { tag: [t.string, t.special(t.string)], color: "var(--success)" },
    { tag: [t.number], color: "var(--warn)" },
    { tag: [t.comment, t.lineComment, t.blockComment], color: "var(--fg-3)", fontStyle: "italic" },
    { tag: [t.propertyName, t.definition(t.propertyName), t.attributeName], color: "var(--info)" },
    { tag: [t.heading], color: "var(--fg-0)", fontWeight: "600" },
    { tag: [t.link, t.url], color: "var(--accent)", textDecoration: "underline" },
    { tag: [t.invalid], color: "var(--danger)" },
  ]);
  const theme = cm.view.EditorView.theme({
    "&": {
      backgroundColor: "var(--bg-3)",
      color: "var(--fg-1)",
      fontSize: "12px",
      border: "1px solid var(--line-2)",
      borderRadius: "var(--r-2)",
    },
    "&.cm-focused": { outline: "2px solid var(--accent-ring)", outlineOffset: "1px" },
    ".cm-scroller": {
      fontFamily: '"JetBrains Mono", "SF Mono", Menlo, Consolas, monospace',
      minHeight: `${minHeight}px`,
      maxHeight: `${maxHeight}px`,
      overflow: "auto",
    },
    ".cm-content": { caretColor: "var(--fg-0)", padding: "8px 0" },
    ".cm-gutters": {
      backgroundColor: "var(--bg-2)",
      color: "var(--fg-4)",
      border: "none",
      borderRight: "1px solid var(--line-1)",
    },
    ".cm-activeLine": { backgroundColor: "color-mix(in srgb, var(--accent) 6%, transparent)" },
    ".cm-activeLineGutter": { backgroundColor: "transparent", color: "var(--fg-2)" },
    ".cm-selectionBackground, &.cm-focused .cm-selectionBackground": {
      backgroundColor: "color-mix(in srgb, var(--accent) 25%, transparent)",
    },
    ".cm-cursor": { borderLeftColor: "var(--fg-0)" },
    ".cm-tooltip": {
      backgroundColor: "var(--bg-1)",
      color: "var(--fg-1)",
      border: "1px solid var(--line-2)",
      borderRadius: "var(--r-2)",
    },
    ".cm-diagnostic-error": { borderLeft: "3px solid var(--danger)" },
    ".cm-diagnostic-warning": { borderLeft: "3px solid var(--warn)" },
    ".cm-diagnostic-info": { borderLeft: "3px solid var(--info)" },
  });
  return [theme, cm.language.syntaxHighlighting(style)];
}

/** diagnosticRange maps a 1-based line / column onto document offsets. */
export function diagnosticRange(
  text: string,
  line: number | undefined,
  column: number | undefined,
): { from: number; to: number } {
  const lines = text.split("\n");
  const li = Math.min(Math.max((line ?? 1) - 1, 0), Math.max(lines.length - 1, 0));
  let from = 0;
  for (let i = 0; i < li; i++) from += lines[i].length + 1;
  const len = lines[li]?.length ?? 0;
  if (column && column > 0) {
    const c = Math.min(column - 1, len);
    return { from: from + c, to: from + Math.min(c + 1, len) };
  }
  return { from, to: from + len };
}

export function CodeEditor({
  value,
  onChange,
  language,
  readOnly,
  diagnostics,
  ariaLabel,
  minHeight = 240,
  maxHeight = 560,
  className,
}: CodeEditorProps) {
  const host = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<import("@codemirror/view").EditorView | null>(null);
  const cmRef = useRef<CM | null>(null);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);

  // Build the editor once per language / read-only switch.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const cm = await loadCM();
        const lang = await languageExtension(cm, language);
        if (cancelled || !host.current) return;
        cmRef.current = cm;
        const state = cm.state.EditorState.create({
          doc: value,
          extensions: [
            cm.view.lineNumbers(),
            cm.view.highlightActiveLine(),
            cm.view.highlightActiveLineGutter(),
            cm.view.drawSelection(),
            cm.commands.history(),
            cm.view.keymap.of([...cm.commands.defaultKeymap, ...cm.commands.historyKeymap, cm.commands.indentWithTab]),
            cm.view.EditorView.lineWrapping,
            cm.state.EditorState.readOnly.of(Boolean(readOnly)),
            cm.view.EditorView.editable.of(!readOnly),
            cm.view.EditorView.contentAttributes.of({ "aria-label": ariaLabel }),
            cm.lint.lintGutter(),
            lang,
            ...themeExtensions(cm, minHeight, maxHeight),
            cm.view.EditorView.updateListener.of((u) => {
              if (u.docChanged) onChangeRef.current?.(u.state.doc.toString());
            }),
          ],
        });
        viewRef.current?.destroy();
        viewRef.current = new cm.view.EditorView({ state, parent: host.current });
        setReady(true);
      } catch {
        if (!cancelled) setFailed(true);
      }
    })();
    return () => {
      cancelled = true;
      viewRef.current?.destroy();
      viewRef.current = null;
      setReady(false);
    };
    // value is synced by the effect below; rebuilding on every keystroke
    // would lose the cursor.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [language, readOnly, ariaLabel, minHeight, maxHeight]);

  // An external value change (a reload, a reset) replaces the document.
  useEffect(() => {
    const v = viewRef.current;
    if (!v) return;
    const cur = v.state.doc.toString();
    if (cur !== value) {
      v.dispatch({ changes: { from: 0, to: cur.length, insert: value } });
    }
  }, [value, ready]);

  // Diagnostics are data: replace the set whenever they change.
  useEffect(() => {
    const v = viewRef.current;
    const cm = cmRef.current;
    if (!v || !cm) return;
    const text = v.state.doc.toString();
    const ds = (diagnostics ?? []).map((d) => {
      const r = diagnosticRange(text, d.line, d.column);
      return { from: r.from, to: r.to, severity: d.severity, message: d.message };
    });
    v.dispatch(cm.lint.setDiagnostics(v.state, ds));
  }, [diagnostics, ready]);

  if (failed) {
    return (
      <textarea
        aria-label={ariaLabel}
        className={clsx(
          "w-full rounded-2 border border-line-2 bg-bg-3 p-2 font-mono text-[12px] text-fg-1",
          className,
        )}
        style={{ minHeight, maxHeight }}
        readOnly={readOnly}
        value={value}
        onChange={(e) => onChange?.(e.target.value)}
      />
    );
  }
  return (
    <div className={clsx("relative min-w-0", className)}>
      <div ref={host} className="min-w-0" />
      {!ready && (
        <div
          className="flex items-center gap-2 rounded-2 border border-line-2 bg-bg-3 px-3 py-2 text-[11.5px] text-fg-3"
          style={{ minHeight }}
        >
          <Spinner size="sm" /> Loading editor
        </div>
      )}
    </div>
  );
}
