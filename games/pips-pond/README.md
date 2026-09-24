# Pip's Pond

A single-file browser game that helps kids practice addition and subtraction.
It has no dependencies and no build step. Open `index.html` in any modern browser.

## Games

| Game | Skill practiced |
|------|-----------------|
| **Lily Hop** | Number-line reasoning. The child predicts which lily pad Pip lands on, then watches Pip count out the hops. |
| **Bubble Pop** | Quick recall. A one-minute round where the child pops the rising bubble that holds the answer. Streaks of correct answers speed the bubbles up. |
| **Pond Race** | Fluency. The child types answers on a big number pad to paddle past Duck. Duck's speed adapts after each race so races stay close. |
| **Fish Friends** | Number bonds. The child finds pairs that add up to a target (make 10, make 100) or that differ by a target. |

## Replayability

- Five number ranges: Tadpole (0–5), Froglet (0–10), Frog (0–20), Bullfrog (0–50) and Pond King (0–100).
- Three modes: adding, taking away, or both.
- Every game awards up to 3 stars, with a best score kept for each game, range and mode.
- Stars unlock 24 stickers and 7 hats for Pip. Kids also see a daily play streak.
- Adaptive practice: missed facts come back more often until they're answered correctly. Grown-ups can see them under **Grown-ups → Tricky facts**.

## iPad and touch support

- Large tap targets, with no hover-only interactions and no on-screen keyboard needed. Pond Race has its own number pad.
- Pinch zoom, double-tap zoom, long-press callouts and text selection are turned off during play.
- The layout uses safe-area insets and adapts to both portrait and landscape.
- Sound effects use the Web Audio API and start on the first tap, as iOS requires.
- To play full screen, open the game in Safari and tap **Share → Add to Home Screen**.

Progress is saved in `localStorage` on the device.

## Hosting

Any static host works: GitHub Pages, Netlify, or `python3 -m http.server` for local testing.
