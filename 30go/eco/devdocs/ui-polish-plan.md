# UI polish plan

Outcome of the designer review and debate round, 2026-08-16. Status:
awaiting approval, nothing implemented yet.

## Agreed items

### 1. Right-aligned user counts in the channel browser

Counts become a scannable column instead of being glued mid-row to the
channel name.

Implementation: in `browser.go`, the row template currently puts
"#name (count)" in the Border's left slot and the topic in the center.
Change the template to three labels: bold name left, topic center
(ellipsis truncation as today), count in a new right slot with
`fyne.TextAlignTrailing`. Update the item-update callback to set the
three labels separately.

### 2. Contextual input placeholder

"Message #general" that updates as you switch buffers, replacing the
static 60-character hint. Confirms where typed text will land.

Implementation: in `ui.go` `showBuffer`, call
`u.input.SetPlaceHolder("Message " + name)` (special case the server
buffer: "Type /help for commands"). Remove the static placeholder from
`NewUI`. Add the Shift+Enter tip to the `/help` output in
`commands.go`.

### 3. Rename the Control Center button to "Networks"

The dialog's own panel header, the sidebar tree root, and the profile
field all already say network; one concept should have one name.

Implementation: in `menu.go`, change the button label
"Servers &amp; Identities" to "Networks". The dialog title keeps the
formal name.

### 4. Pane width rebalance plus ellipsis truncation

Channel sidebar from 170 to 200px (channel names often exceed 170 and
currently clip with no ellipsis), member list from 170 to 150px (nick
columns rarely need the width).

Implementation: in `ui.go`, adjust the two `SetMinSize` calls
(`sideWidth` to 200, `memberWidth` to 150), and set
`Truncation = fyne.TextTruncateEllipsis` on both the `bufferRow` label
and the member list row template.

### 5. Line counter warns near the cap

"20/24 lines" renders in the warning color from max minus 4 onward, so
truncation stops arriving unannounced. Neutral grey below that.

Implementation: in `ui.go` `updateComposeHint`, set
`u.composeHint.Importance = widget.WarningImportance` when
`lines >= max-4`, `widget.MediumImportance` otherwise, then refresh the
label.

## Optional item, pending explicit approval

### 6. Bold member rows for anyone with a mode prefix

Rank sorting puts ops at the top, but mid-scroll in a large channel the
rank boundaries are invisible. Bolding rows that carry any prefix makes
the boundary visible with no layout change.

Implementation: in `ui.go`, the member list update callback checks
whether the row text starts with a prefix character (`~&amp;@%+`) and sets
`TextStyle{Bold: true}` accordingly. Single label, click-to-PM nick
extraction unchanged.

## Killed in debate

- Focusing the browser search on open: already shipped this session,
  the reviewer had a stale mental model of `browser.go`.
- Unselecting the sidebar row on disconnect: selection means "what the
  pane is showing", and after a disconnect the pane genuinely shows the
  server log. Unselecting would create the disagreement it claimed to
  fix. Connection health is the status dot's job. Withdrawn by the
  designer.
- Two-label styled member prefixes: redundant next to rank sorting and
  complicates nick extraction. Reduced to optional item 6.
- Compose hint "appear at 2 lines": already the current behavior,
  reduced to item 5.
