# Nug's Pond

A single-file browser game that helps kids practice addition and subtraction.
It has no dependencies and no build step. Open `index.html` in any modern browser.

## Games

| Game | Skill practiced |
|------|-----------------|
| **Lily Hop** | Number-line reasoning. The child predicts which lily pad Nug lands on, then watches Nug count out the hops. A right guess makes the answer float up off its pad into the sky in green, growing as it goes. A wrong guess shows a red "Oops! Not quite" banner. The picked pad turns red with a ✕, the correct pad turns green with a ✓, and a message explains the right answer. The game is set in a full pond scene with sun, drifting clouds, hills, cattails, lotus flowers and butterflies. |
| **Bubble Pop** | Quick recall. A one-minute round where the child pops the rising bubble that holds the answer. A correct bubble turns green, grows and bursts into stars. Bubbles start slow, taking 12 seconds to cross the tank (14 at the bigger number sizes). Every 5 correct answers is a new speed level, each about 10% faster, and bubbles never take less than 6.5 seconds. |
| **Pond Race** | Fluency. The child picks a racer from eight characters, then types answers on a big number pad and taps **Enter** to check (a keyboard's Enter key works too). This lets them paddle past Duck. A wrong answer shows a red X and offers **Try again** or **Show me**, and Duck waits meanwhile. Duck's speed adapts after each race so races stay close. |
| **Fish Friends** | Number bonds. Each round shows a math sentence with empty boxes, like `▢ + ▢ = 10` or `▢ − ▢ = 3`. Tapping a fish drops its number into a box, and a wrong pair shows what it actually makes. |

## Replayability

- Five number ranges: Tadpole (0–5), Froglet (0–10), Frog (0–20), Bullfrog (0–50) and Pond King (0–100).
- Every game mixes adding and taking away evenly.
- Every game awards up to 3 stars, with a best score kept for each game, range and mode.
- Stars unlock 48 stickers and 7 hats for Nug. Every sticker makes its own sound, like "Ribbit!" or "Moo!", with a speech bubble: when it opens, when you tap 🔊 Hear it, and when you tap it on a sticker page. Tapping a sticker in the sticker book zooms it up into a thick, two-sided 3D sticker. Kids can drag it to spin it any direction, or tap **Spin it!** for a big twirl. **Add to a page** puts the sticker straight onto the current sticker page. A sticker earned at the end of a game shows up on the results screen as a card, and tapping the card opens that sticker in the same viewer.
- **Sticker pages** (Stickers & hats → Decorate sticker pages): a scrapbook where kids place their unlocked stickers on pages. They can tap or drag stickers onto a page, drag to move them, and turn or resize them with the yellow handle or a two-finger pinch. They can remove a sticker with ✕, pick a background (Pond, Sky, Meadow, Night, Sunset or Paper) and add up to 30 pages. Every change saves automatically. Kids also see a daily play streak.
- Adaptive practice: missed facts come back more often until they're answered correctly. Grown-ups can see them under **Grown-ups → Tricky facts**.

## Discouraging panic guessing

A wrong answer given very quickly after a question appears counts as a rushed guess. The limits are 1.8s in Lily Hop, 1.5s in Bubble Pop and Pond Race, and 2s in Fish Friends.

- The first rushed guess shows a gentle "Take your time!" reminder.
- A second rushed guess within 20 seconds covers the game with a **"Slow down!"** pause: 3 seconds the first time, 5 seconds after that. The game can't be tapped during the pause, but the Bubble Pop timer and Duck keep going, so rushing costs something.
- Guessing again and again doesn't work:
  - **Bubble Pop:** one tap per question. A wrong tap ends that question and highlights the right bubble.
  - **Pond Race:** two tries per problem. After that, the answer is shown and the race moves on.
  - **Fish Friends:** three tries per round. After that, the game shows a correct pair and moves on.

## iPad and touch support

- Large tap targets, with no hover-only interactions and no on-screen keyboard needed. Pond Race has its own number pad.
- Pinch zoom, double-tap zoom, long-press callouts and text selection are turned off during play.
- The layout uses safe-area insets and adapts to both portrait and landscape.
- Sound effects use the Web Audio API and start on the first tap, as iOS requires.
- To play full screen, open the game in Safari and tap **Share → Add to Home Screen**.

Progress is saved in `localStorage` on the device.

## Hosting

Any static host works: GitHub Pages, Netlify, or `python3 -m http.server` for local testing.
