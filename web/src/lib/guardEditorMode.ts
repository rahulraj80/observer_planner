// guardEditorMode: what the Security > Rules override editor shows for each
// way it is opened (T-762, D-580). The editor used to infer "editing" from the
// presence of an initial row, but "+ Add override" always passed a blank
// initial row, so the Rule picker opened locked and no rule could be chosen.
// The opener now says what it is doing, and this table decides the rest:
//
//   add     "+ Add override": blank row, the rule is chosen here.
//   preset  "Override" on an effective-rules row: a NEW override already
//           aimed at that rule, so the rule stays locked to it.
//   edit    "Edit" on an existing override row: the rule is the row's key,
//           so it stays locked; the button saves instead of adding.

export type OverrideEditorMode = "add" | "preset" | "edit";

export type OverrideEditorView = {
  ruleLocked: boolean;
  title: string;
  saveLabel: string;
  isNew: boolean;
};

export function overrideEditorView(mode: OverrideEditorMode, rule: string): OverrideEditorView {
  switch (mode) {
    case "edit":
      return { ruleLocked: true, title: `Edit override - ${rule}`, saveLabel: "Save override", isNew: false };
    case "preset":
      return { ruleLocked: true, title: `Override ${rule}`, saveLabel: "Add override", isNew: true };
    default:
      return { ruleLocked: false, title: "Override a built-in rule", saveLabel: "Add override", isNew: true };
  }
}
