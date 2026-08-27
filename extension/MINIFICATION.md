# bookmarklet.js -> bookmarklet.compact.js -> bookmarklet.min.js

Three files, three jobs. Don't hand-edit `bookmarklet.min.js` — regenerate it
from `bookmarklet.compact.js` via uglify-js instead (command at the bottom).

## bookmarklet.js (10712 bytes) — source of truth

Fully readable: full identifiers (`estsauth`, `estsauthpersistent`, `secret`,
`endpoint`, `persistentSkipped`, `lastSeenClip`), comments, template-literal
HTML with indentation. This is what you read, review, and diff. Never
minified by hand — always the input to the next stage.

## bookmarklet.compact.js (7298 bytes) — manual compaction

Same logic, restructured by hand to remove *redundancy a minifier can't see*
— things that require understanding what the code means, not just its
syntax:

- **Shared string constants.** `"M365Bridge"`/`"m365bridge"` appeared 7
  times across brand label, localStorage key prefixes, and the postMessage
  channel name -> one `BRAND`/`brand` pair. `"ESTSAUTH"` / `"ESTSAUTHPERSISTENT"`
  appeared 24 / 13 times across status messages, JSON field names, and field
  labels -> `EA` / `EP = EA + "PERSISTENT"`. A minifier can't do this because
  it doesn't know two string literals with the same characters are the same
  *concept* — it can only dedupe by turning repeated literals into a variable
  if they're byte-identical, and even then most minifiers don't bother for
  strings (this isn't like constant-folding numbers).
- **Short internal keys.** The `S` state object's properties went from
  `estsauth`/`estsauthpersistent`/`secret`/`endpoint`/`persistentSkipped`/
  `lastSeenClip` to `a`/`p`/`s`/`e`/`k`/`c`. This is NOT something terser's
  `mangle` does — `mangle` only renames local variable/function *bindings*
  it can prove are safe to rename; object property names and string keys
  are potentially part of an external contract (JSON, DOM, serialization)
  so they're left alone unless you opt into `mangle.properties`, which is
  unsafe here (`dataset.key` values, JSON field names sent over the wire,
  and localStorage keys all flow through the same object shape and would
  break silently). Shortening them by hand, with full knowledge of which
  keys are load-bearing (JSON payload field names like `"ESTSAUTH"` in the
  wire format) versus purely internal (`S.a`), is a call only a human/LLM
  reading the code can make safely.
- **Repeated subproperty access hoisted to locals.** `window` / `document`
  -> `W` / `D`; `navigator.clipboard` -> `clip`; `crypto.subtle` -> `sc`.
  Also collapsed repeated call patterns into helpers: `el.querySelector`
  -> `qs(el, sel)`, `el.addEventListener`/`removeEventListener` -> `on`/`off`,
  `crypto.getRandomValues(new Uint8Array(n))` -> `rnd(n)`,
  `btoa(String.fromCharCode(...new Uint8Array(buf)))` -> `b64(buf)`.
  A minifier's compressor pass *can* sometimes do a limited version of this
  (e.g. Closure Compiler's collapse-properties), but terser's `compress`
  does not hoist repeated member expressions into locals — that's a
  semantic judgment call about which accesses are "the same reference" and
  worth naming, not a mechanical transform.
- **HTML/CSS de-duplication.** `field()`/`FIELDS` build the three cookie
  inputs from one template instead of three near-identical literals;
  `btn()`/`lbl()` factor out the repeated `<button ...>`/`<label class=f
  data-key=...>` wrapper markup. `.t{display:block;...}` and
  `.f{display:block;...}` merged their shared `display:block` into one
  `.t,.f{display:block}` rule (kept the differing `font-weight`/`margin`
  rules separate — an earlier attempt at this collapsed them into one
  shared block and silently dropped that styling, which is the risk of
  doing this by hand without re-diffing behavior afterward). Removed
  `autocomplete=off` from the dynamically-created `<input>`s: without a
  `name`/`form` and outside a submitted `<form>`, both Chromium and Firefox
  ignore the attribute for anything they'd consider autofill-worthy, so it
  was dead weight, not a real behavior toggle. None of this is something a
  JS minifier touches — it operates on the JS AST, not on markup sitting
  inside a template-literal string.

Verify after every hand edit: `node --check bookmarklet.compact.js`,
then re-diff behavior against `bookmarklet.js` (same status messages, same
field order, same visual result) before regenerating the `.min.js`.

## bookmarklet.min.js (4519 bytes) — mechanical minification

Generated from `bookmarklet.compact.js` by uglify-js, which does the parts
that ARE purely mechanical and safe to automate:

- Renames local variable/function bindings to 1-2 character names
  (`mangle`) — safe because uglify-js proves each one is a local binding
  with no outside references (not a property, not a global, not a string
  key).
- Strips whitespace, comments, and redundant syntax (`compress`) — dead
  code elimination, shorthand operators, collapsing `if`/`return` patterns,
  etc.
- Does NOT touch string contents, object property names, or markup inside
  template literals — which is exactly why the hand-compaction stage above
  has to happen first. Running a minifier directly on `bookmarklet.js`
  (skipping the compaction stage) was tried with both terser and uglify-js
  for comparison — see the tool trial below — and got source down to
  ~5779–5835 bytes purely mechanically, but left every repeated
  `"ESTSAUTH"`/`"M365Bridge"` literal and every `estsauthpersistent`-length
  object key untouched, because none of that is a minifier's job.

### Why uglify-js over terser

Originally used terser (4594 bytes from `bookmarklet.compact.js`). Tried
uglify-js, esprima(+escodegen), and google-closure-compiler side by side as
alternatives — footprint and output size for each, run against
`bookmarklet.compact.js`:

| tool | unpacked footprint | deps | output size |
|---|---:|---:|---:|
| **uglify-js** (now used) | ~1.3 MB | 0 | **4519 bytes** |
| terser (previous) | ~2.3 MB | 4 | 4594 bytes |
| google-closure-compiler, `SIMPLE_OPTIMIZATIONS` | ~70 MB (JS wrapper + native Java binary) | 5 + platform binaries | 5116 bytes |
| esprima + escodegen | ~0.4 MB combined | 4 (escodegen's) | N/A — can't parse the file |

uglify-js won on every axis: smallest output, lightest footprint, zero
dependencies. Switched the pipeline to it.

Closure Compiler's `ADVANCED_OPTIMIZATIONS` mode (which is where its
reputation for aggressive minification comes from) was not usable here: it
requires full browser API externs to avoid `JSC_UNDEFINED_VARIABLE` errors,
and more importantly it renames object properties by default — unsafe for
this file, for the same reason terser's `mangle.properties` was ruled out
(`S[key]`/`STORE[key]`/`dataset.key` all do dynamic property lookups by
string that a properties-renaming pass can't see through). Its safe tier,
`SIMPLE_OPTIMIZATIONS`, still lost to both terser and uglify-js on size
despite by far the heaviest footprint of the four.

esprima can only parse — it has no compress/mangle pass of its own. The
`esprima.org` online "minify" demo people remember doesn't run esprima
alone; it runs **esmangle**, a separate esprima-based minifier. esmangle's
last real release was v1.0.1 in April 2014, and it bundles its own ancient
`esprima ~1.1.1`, so — like a bare `esprima` install — it can't parse
ES2019 optional-catch-binding (`catch { }`) or ES2020 optional chaining
(`?.`), both used in this file. Ruled out as effectively unmaintained
tooling rather than a viable alternative.

Regenerate with:

```
npx --yes uglify-js bookmarklet.compact.js --compress --mangle -o bookmarklet.min.js
```

Always follow with `node --check bookmarklet.min.js` to confirm the output
still parses.

## Net result

| file                    | bytes | bytes cut by this stage | role                             |
|-------------------------|------:|-------------------------:|-----------------------------------|
| bookmarklet.js          | 10712 |                        — | source of truth, hand-edited      |
| bookmarklet.compact.js  |  7298 |     3414 (manual, from bookmarklet.js) | hand-compacted intermediate       |
| bookmarklet.min.js      |  4519 |     2779 (uglify-js, from compact.js)  | uglify-js output, paste into a bookmark |

10712 bytes -> 4519 bytes overall: 6193 bytes removed, 57.8% smaller than
the original. Of those 6193 bytes, manual compaction cut 3414 and
uglify-js cut 2779 — the two stages are close to even, with manual
compaction doing slightly more (55.1% of the 6193-byte reduction came from
compaction, 44.9% from uglify-js).
