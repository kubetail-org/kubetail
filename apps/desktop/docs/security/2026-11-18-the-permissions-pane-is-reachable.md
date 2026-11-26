# Security record — the Permissions pane is reachable, 18 November 2026

**Subject:** the Settings dialog's Permissions tab draws the permissions pane, and a rule can be
edited from it. The engine is [the permissions engine](2026-10-02-the-permissions-engine.md). The
living model is [security-model.md](../security-model.md).

## What changes

- The pane, built and tested before, is now reachable from Settings.
- A rule's `…` menu offers Edit beside Remove. Edit is `permissionRuleAdd` with the edited rule,
  then `permissionRuleRemove` of the original, sent only once the add came back without a refusal.
- *Test a request*, the *Showing* filter, *Chat grants* and *Ran without asking* are drawn as
  placeholders. None sends a query or a mutation, and none decides an outcome.

## What holds

- No mutation, grant path or approval path is added. The pane calls the six `permission*`
  mutations it called before.
- An edit never widens past the two rules it touches. A verdict holds under any order of the
  rules and Deny wins (`TestAVerdictIsOrderIndependent`, `TestDenyWinsOverAskWinsOverAllow`), so
  the moment both rules are in the file decides nothing either would not.
- A refused add removes nothing (`removes nothing when an edit is refused, and says why`). Save is
  held while the draft equals the rule, since an add of a rule the file holds writes nothing and
  the remove would then delete the original.
- Every rule control is held while the rules field is one Kubetail cannot read.
- No webview code matches a pattern or decides a permission; the engine stays in the sidecar.
- Every context, pattern, rule line and refused value is drawn through `VisibleText`; a `<select>`
  option, which holds text alone, is spelled out by `visibleTitle` (`field.tsx`'s `Select`).

## Residual risk

Between the add and the remove the file holds both rules. A crash there leaves both, which the
user sees and can remove; it grants nothing either rule did not grant alone.
