# Pip's Pond

A single-file browser game that helps kids practice addition and subtraction.
It has no dependencies and no build step. Open `index.html` in any modern browser.

## Games

| Game | Skill practiced |
|------|-----------------|
| **Lily Hop** | Number-line reasoning. The child predicts which lily pad Pip lands on, then watches Pip count out the hops. The answer then floats up off its pad into the sky, growing as it goes. The game is set in a full pond scene with sun, drifting clouds, hills, cattails, lotus flowers and butterflies. |
| **Bubble Pop** | Quick recall. A one-minute round where the child pops the rising bubble that holds the answer. A correct bubble turns green, grows and bursts into stars. Every 5 correct answers is a new speed level, and the bubbles rise faster. |
| **Pond Race** | Fluency. The child picks a racer from eight characters, then types answers on a big number pad to paddle past Duck. A wrong answer shows a red X and offers **Try again** or **Show me**, and Duck waits meanwhile. Duck's speed adapts after each race so races stay close. |
| **Fish Friends** | Number bonds. Each round shows a math sentence with empty boxes, like `▢ + ▢ = 10` or `▢ − ▢ = 3`. Tapping a fish drops its number into a box, and a wrong pair shows what it actually makes. |

## Replayability

- Five number ranges: Tadpole (0–5), Froglet (0–10), Frog (0–20), Bullfrog (0–50) and Pond King (0–100).
- Every game mixes adding and taking away evenly.
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
