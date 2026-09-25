// In jsdom, CodeMirror's contenteditable does not accept `userEvent.type`
// reliably. YamlEditor therefore renders a plain `<textarea>` under Vitest
// (import.meta.env.MODE === "test") instead of mounting CodeMirror. The real
// CodeMirror path is exercised by the Playwright suite in Task 10.
import { useEffect, useRef } from "react";
import { EditorState, StateEffect, StateField } from "@codemirror/state";
import { Decoration, type DecorationSet, EditorView } from "@codemirror/view";
import { basicSetup } from "codemirror";
import { yaml } from "@codemirror/lang-yaml";

const setErrorLine = StateEffect.define<number | null>();

const errorLineField = StateField.define<DecorationSet>({
  create() {
    return Decoration.none;
  },
  update(decorations, tr) {
    let next = decorations.map(tr.changes);
    for (const effect of tr.effects) {
      if (effect.is(setErrorLine)) {
        if (effect.value == null) {
          next = Decoration.none;
        } else {
          const lineNumber = Math.min(Math.max(effect.value, 1), tr.state.doc.lines);
          const line = tr.state.doc.line(lineNumber);
          next = Decoration.set([Decoration.line({ class: "cm-error-line" }).range(line.from)]);
        }
      }
    }
    return next;
  },
  provide: (field) => EditorView.decorations.from(field),
});

export interface YamlEditorProps {
  value: string;
  onChange: (value: string) => void;
  ariaLabel: string;
  errorLine?: number;
}

const isTest = import.meta.env.MODE === "test";

export default function YamlEditor({ value, onChange, ariaLabel, errorLine }: YamlEditorProps) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<EditorView | null>(null);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  useEffect(() => {
    if (isTest || !hostRef.current) return;
    const view = new EditorView({
      state: EditorState.create({
        doc: value,
        extensions: [
          basicSetup,
          yaml(),
          errorLineField,
          EditorView.contentAttributes.of({ "aria-label": ariaLabel, role: "textbox", "aria-multiline": "true" }),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) onChangeRef.current(update.state.doc.toString());
          }),
        ],
      }),
      parent: hostRef.current,
    });
    viewRef.current = view;
    return () => {
      view.destroy();
      viewRef.current = null;
    };
    // Mounted once; `value` only seeds the initial document, and later
    // updates come from the effects below so the cursor isn't reset on
    // every keystroke.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ariaLabel]);

  useEffect(() => {
    if (isTest) return;
    const view = viewRef.current;
    if (!view) return;
    const current = view.state.doc.toString();
    if (current !== value) {
      view.dispatch({ changes: { from: 0, to: current.length, insert: value } });
    }
  }, [value]);

  useEffect(() => {
    if (isTest) return;
    const view = viewRef.current;
    if (!view) return;
    view.dispatch({ effects: setErrorLine.of(errorLine ?? null) });
  }, [errorLine]);

  if (isTest) {
    return (
      <textarea
        aria-label={ariaLabel}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="min-h-64 w-full rounded-md border p-3 font-mono text-sm"
      />
    );
  }

  return <div ref={hostRef} />;
}
